package agentsession

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

//go:generate mockgen -source=session.go -destination=mock_agentsession/mock_session.go -package=mock_agentsession

var errMissedHeartbeats = errors.New("missed agent heartbeats")

type Session interface {
	ID() string
	Run(ctx context.Context) error
	Write(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error
}

type session struct {
	id               string
	conn             *websocket.Conn
	logger           log.Logger
	registry         Registry
	writeMu          sync.Mutex
	jobRepository    repository.JobRepository
	lockRepository   repository.LockRepository
	scmPublisher     scm.Publisher
	jobService       services.JobService
	pingInterval     time.Duration
	pongTimeout      time.Duration
	missedHeartbeats int
	pongCh           chan struct{}
}

func (s *session) ID() string {
	return s.id
}

func (s *session) Run(ctx context.Context) (err error) {
	defer func() {
		_ = s.registry.Unregister(ctx, s.id)
		if err != nil {
			// Error teardown: skip the handshake wait; the peer already saw a fault.
			_ = s.conn.CloseNow()
			return
		}
		// Clean exit (expected disconnect / ctx cancel): send a normal close so the
		// agent treats this as an expected disconnect rather than a read error.
		_ = s.conn.Close(websocket.StatusNormalClosure, "")
	}()

	s.logger.Info("Agent session started", "agent_id", s.id)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gCtx := errgroup.WithContext(runCtx)
	g.Go(func() error {
		defer cancel()
		return s.readLoop(gCtx)
	})
	if s.pingInterval > 0 {
		g.Go(func() error {
			defer cancel()
			return s.heartbeatLoop(gCtx)
		})
	}

	err = g.Wait()
	if errors.Is(err, errMissedHeartbeats) {
		s.logger.Debug("Agent session closed after missed heartbeats", "agent_id", s.id)
		err = nil
		return nil
	}
	if err != nil && !isExpectedDisconnect(err) && ctx.Err() == nil {
		return err
	}

	// Clear named result so deferred teardown uses a normal close.
	err = nil
	s.logger.Info("Agent session closed", "agent_id", s.id)
	return nil
}

func (s *session) readLoop(ctx context.Context) error {
	for {
		env, err := wsproto.ReadEnvelope(ctx, s.conn)
		if err != nil {
			if isExpectedDisconnect(err) || ctx.Err() != nil {
				return err
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		if err := s.handleWebsocketPayload(ctx, env); err != nil {
			return err
		}
	}
}

func (s *session) handleWebsocketPayload(ctx context.Context, env *terraplanev1.WebsocketEnvelope) error {
	switch env.GetPayload().(type) {
	case *terraplanev1.WebsocketEnvelope_Pong:
		s.logger.Debug("Received agent pong", "agent_id", s.id)
		select {
		case s.pongCh <- struct{}{}:
		default:
		}
	case *terraplanev1.WebsocketEnvelope_Ping:
		s.logger.Debug("Ignoring unexpected ping from agent", "agent_id", s.id)
	case *terraplanev1.WebsocketEnvelope_Terraform:
		tf := env.GetTerraform()
		if tf == nil {
			s.logger.Debug("Ignoring empty terraform payload", "agent_id", s.id)
			return nil
		}
		if err := s.handleTerraform(ctx, tf); err != nil {
			return err
		}
		s.logger.Info("Received websocket message", "agent_id", s.id, "message", tf.String())
	default:
		s.logger.Debug("Ignoring unexpected websocket payload", "agent_id", s.id, "payload", fmt.Sprintf("%T", env.GetPayload()))
	}
	return nil
}

func (s *session) handleTerraform(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	switch msg.GetPayload().(type) {
	case *terraplanev1.TerraformEnvelope_Ack:
		if err := s.handleAck(ctx, msg); err != nil {
			return fmt.Errorf("error handling ack from agent %s: %w", s.id, err)
		}
	case *terraplanev1.TerraformEnvelope_PlanResult:
		if err := s.handlePlanResult(ctx, msg); err != nil {
			return fmt.Errorf("error handling plan result from agent %s: %w", s.id, err)
		}
	case *terraplanev1.TerraformEnvelope_ApplyResult:
		if err := s.handleApplyResult(ctx, msg); err != nil {
			return fmt.Errorf("error handling apply result from agent %s: %w", s.id, err)
		}
	}
	return nil
}

func (s *session) heartbeatLoop(ctx context.Context) error {
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()

	pongTimer := time.NewTimer(s.pongTimeout)
	defer pongTimer.Stop()

	missed := 0
	ping := &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Ping{Ping: &terraplanev1.Ping{}},
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Drop a stale pong from a previous interval.
			select {
			case <-s.pongCh:
			default:
			}

			s.logger.Debug("Sending agent ping", "agent_id", s.id)
			if err := s.writeMsg(ctx, ping); err != nil {
				return fmt.Errorf("write agent ping: %w", err)
			}

			drainTimer(pongTimer)
			pongTimer.Reset(s.pongTimeout)
			select {
			case <-s.pongCh:
				s.logger.Debug("Agent heartbeat ok", "agent_id", s.id)
				missed = 0
			case <-pongTimer.C:
				missed++
				s.logger.Debug(
					"Agent heartbeat missed",
					"agent_id", s.id,
					"missed", missed,
					"max", s.missedHeartbeats,
				)
				if missed >= s.missedHeartbeats {
					return errMissedHeartbeats
				}
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func drainTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func (s *session) handleAck(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	return s.jobService.AckJob(ctx, msg.GetJobId())
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

	// TODO: This is not an ideal place to publish to the SCM provider, but for now lets do it here
	comment := feedback.PlanResultComment(job, planResult.GetSuccess(), planResult.GetOutput(), planResult.GetError())
	if err := s.scmPublisher.WriteComment(ctx, job.Repo, int(job.PRNumber), comment); err != nil {
		s.logger.Error(
			"Failed to write plan result comment",
			"job_id", jobId,
			"repo", job.Repo,
			"pr", job.PRNumber,
			"stack", job.StackName,
			"error", err,
		)
	}

	return nil
}

func (s *session) handleApplyResult(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	jobId := msg.GetJobId()
	job, err := s.jobRepository.Get(ctx, jobId)
	if err != nil {
		return fmt.Errorf("failed to fetch job %s: %w", jobId, err)
	}

	applyResult := msg.GetApplyResult()
	if applyResult == nil {
		job.Status = models.JobStatusFailed
		job.ErrorMsg = "apply result is nil"
	} else if applyResult.GetSuccess() {
		job.Status = models.JobStatusSucceeded
		job.Output = applyResult.Output
		job.ErrorMsg = applyResult.Error
	} else {
		job.Status = models.JobStatusFailed
		job.Output = applyResult.Output
		job.ErrorMsg = applyResult.Error
	}

	if err := s.jobRepository.Update(ctx, job); err != nil {
		if releaseErr := s.releaseApplyLock(ctx, job, jobId); releaseErr != nil {
			return fmt.Errorf(
				"failed to update job %s with apply result: %w (also failed to release lock: %v)",
				jobId, err, releaseErr,
			)
		}
		return fmt.Errorf("failed to update job %s with apply result: %w", jobId, err)
	}

	if err := s.releaseApplyLock(ctx, job, jobId); err != nil {
		return err
	}

	if applyResult == nil {
		return errors.New("apply result is nil")
	}

	// TODO: This is not an ideal place to publish to the SCM provider, but for now lets do it here
	comment := feedback.ApplyResultComment(job, applyResult.GetSuccess(), applyResult.GetOutput(), applyResult.GetError())
	if err := s.scmPublisher.WriteComment(ctx, job.Repo, int(job.PRNumber), comment); err != nil {
		s.logger.Error(
			"Failed to write apply result comment",
			"job_id", jobId,
			"repo", job.Repo,
			"pr", job.PRNumber,
			"stack", job.StackName,
			"error", err,
		)
	}

	return nil
}

func (s *session) releaseApplyLock(ctx context.Context, job *models.Job, jobID string) error {
	if err := s.lockRepository.Delete(ctx, job.Repo, job.StackName, "default"); err != nil {
		return fmt.Errorf("failed to release lock for job %s stack %q: %w", jobID, job.StackName, err)
	}
	s.logger.Debug("Released lock for job", "job_id", jobID, "repo", job.Repo, "stack", job.StackName)
	return nil
}

func (s *session) Write(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	return s.writeMsg(ctx, wsproto.WrapTerraform(msg))
}

func (s *session) writeMsg(ctx context.Context, msg proto.Message) error {
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
