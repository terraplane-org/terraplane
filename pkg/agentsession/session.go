package agentsession

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

type Session interface {
	ID() string
	Run(ctx context.Context) error
	Write(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error
}

type session struct {
	id             string
	conn           *websocket.Conn
	logger         log.Logger
	registry       Registry
	writeMu        sync.Mutex
	jobRepository  repository.JobRepository
	lockRepository repository.LockRepository
}

func (s *session) ID() string {
	return s.id
}

func (s *session) Run(ctx context.Context) error {
	defer func() {
		_ = s.registry.Unregister(ctx, s.id)
		_ = s.conn.Close(websocket.StatusNormalClosure, "")
	}()

	s.logger.Info("Agent session started", "agent_id", s.id)

	for {
		var msg terraplanev1.TerraformEnvelope
		err := wsproto.Read(ctx, s.conn, &msg)
		if err != nil {
			if isExpectedDisconnect(err) || ctx.Err() != nil {
				s.logger.Info("Agent session closed", "agent_id", s.id)
				return nil
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		// TODO: Move handlers to somewhere else
		switch msg.GetPayload().(type) {
		case *terraplanev1.TerraformEnvelope_Ack:
			if err := s.handleAck(ctx, &msg); err != nil {
				return fmt.Errorf("error handling ack from agent %s: %w", s.id, err)
			}
		case *terraplanev1.TerraformEnvelope_PlanResult:
			if err := s.handlePlanResult(ctx, &msg); err != nil {
				return fmt.Errorf("error handling plan result from agent %s: %w", s.id, err)
			}
		}
		s.logger.Info("Received websocket message", "agent_id", s.id, "message", msg.String())
	}
}

func (s *session) handleAck(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	jobId := msg.GetJobId()
	job, err := s.jobRepository.Get(ctx, jobId)
	if err != nil {
		return fmt.Errorf("failed to fetch job %s: %w", jobId, err)
	}

	job.Status = models.JobStatusRunning
	if err := s.jobRepository.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job %s status to running: %w", jobId, err)
	}

	return nil
}

func (s *session) handlePlanResult(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	jobId := msg.GetJobId()
	job, err := s.jobRepository.Get(ctx, jobId)

	if err != nil {
		return fmt.Errorf("failed to fetch job %s: %w", jobId, err)
	}

	planResult := msg.GetPlanResult()
	if planResult == nil {
		return errors.New("plan result is nil")
	}

	if planResult.GetSuccess() {
		job.Status = models.JobStatusSucceeded
	} else {
		job.Status = models.JobStatusFailed
	}
	job.Output = planResult.Output
	job.ErrorMsg = planResult.Error

	if err := s.jobRepository.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job %s with plan result: %w", jobId, err)
	}

	if err := s.lockRepository.Delete(ctx, job.Repo, job.StackName, "default"); err != nil {
		return fmt.Errorf("failed to release lock for job %s stack %q: %w", jobId, job.StackName, err)
	}
	s.logger.Debug("Released lock for job", "job_id", jobId, "repo", job.Repo, "stack", job.StackName)

	return nil
}

func (s *session) Write(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return wsproto.Write(ctx, s.conn, msg)
}

func isExpectedDisconnect(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return true
	default:
		return false
	}
}
