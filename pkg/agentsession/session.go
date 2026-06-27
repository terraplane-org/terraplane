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

		switch msg.GetPayload().(type) {
		case *terraplanev1.TerraformEnvelope_Ack:
			if err := s.handleAck(ctx, &msg); err != nil {
				return fmt.Errorf("error handling ack from agent %s: %w", s.id, err)
			}
		}

		s.logger.Info("Received websocket message", "agent_id", s.id, "message", msg.String())
	}
}

func (s *session) handleAck(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	// TODO: What should we do if any of this fails? Cancel the TF plan somehow?
	// TODO: Maybe this should be its own service, but for now, we can just update the job status here and create a lock

	jobId := msg.GetJobId()
	job, err := s.jobRepository.Get(ctx, jobId)

	if err != nil {
		// TODO: Should we cancel the job here if we can't find it?
		return fmt.Errorf("failed to fetch job %s: %w", jobId, err)
	}

	// TODO: This assumes that the job is always a plan job. We should probably check the job type and only create a lock for plan jobs.
	// Create a lock first
	lock := &models.ProjectLock{
		Repo:      job.Repo,
		StackName: job.StackName,
		Workspace: "default",
		Dir:       job.Dir,
		CommitSHA: job.CommitSHA,
		LockedBy:  s.id, // TODO: This should be the triggering user ID
		PRNumber:  job.PRNumber,
	}

	err = s.lockRepository.Create(ctx, lock)
	if err != nil {
		return fmt.Errorf("failed to create lock for job %s: %w", jobId, err)
	}

	// Update the job status to running
	job.Status = models.JobStatusRunning
	err = s.jobRepository.Update(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to update job %s status to running: %w", jobId, err)
	}

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
