package services

import (
	"context"
	"fmt"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
)

type JobClaimService interface {
	// Claim waits up to wait for a job for agentID, or returns nil when none is available.
	Claim(ctx context.Context, agentID string, wait time.Duration) (*models.Job, error)
}

type jobClaimService struct {
	logger log.Logger
	jobs   repository.JobRepository
	lease  time.Duration
	poll   time.Duration
}

func NewJobClaimService(logger log.Logger, jobs repository.JobRepository, lease time.Duration) JobClaimService {
	if lease <= 0 {
		lease = time.Hour
	}
	return &jobClaimService{
		logger: logger,
		jobs:   jobs,
		lease:  lease,
		poll:   time.Second,
	}
}

func NewJobClaimServiceFromConfig(logger log.Logger, jobs repository.JobRepository, cfg *config.Config) JobClaimService {
	return NewJobClaimService(logger, jobs, cfg.OrchestratorJobLeaseDuration)
}

func (s *jobClaimService) Claim(ctx context.Context, agentID string, wait time.Duration) (*models.Job, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	if wait < 0 {
		wait = 0
	}

	deadline := time.Now().Add(wait)
	for {
		job, err := s.jobs.ClaimNext(ctx, agentID, s.lease)
		if err != nil {
			return nil, err
		}
		if job != nil {
			s.logger.Info(
				"Agent claimed job",
				"agent_id", agentID,
				"job_id", job.ID,
				"action", job.Action,
				"repo", job.Repo,
				"stack", job.StackName,
			)
			return job, nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}

		sleep := s.poll
		if sleep > remaining {
			sleep = remaining
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type JobResultService interface {
	Complete(ctx context.Context, agentID, jobID string, success bool, output, errMsg string) error
	FailExpired(ctx context.Context) error
}

type jobResultService struct {
	logger       log.Logger
	jobs         repository.JobRepository
	locks        repository.LockRepository
	scmPublisher scm.Publisher
}

func NewJobResultService(
	logger log.Logger,
	jobs repository.JobRepository,
	locks repository.LockRepository,
	scmPublisher scm.Publisher,
) JobResultService {
	return &jobResultService{
		logger:       logger,
		jobs:         jobs,
		locks:        locks,
		scmPublisher: scmPublisher,
	}
}

func (s *jobResultService) Complete(ctx context.Context, agentID, jobID string, success bool, output, errMsg string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to fetch job %s: %w", jobID, err)
	}
	if job.AgentID != agentID {
		return fmt.Errorf("job %s is not assigned to agent %q", jobID, agentID)
	}
	if job.Status != models.JobStatusRunning && job.Status != models.JobStatusPending {
		return fmt.Errorf("job %s is not active (status %s)", jobID, job.Status)
	}

	if success {
		job.Status = models.JobStatusSucceeded
	} else {
		job.Status = models.JobStatusFailed
	}
	job.Output = output
	job.ErrorMsg = errMsg
	job.LeaseExpiresAt = nil

	if err := s.jobs.Update(ctx, job); err != nil {
		if job.Action == models.JobActionApply {
			if releaseErr := s.releaseApplyLock(ctx, job); releaseErr != nil {
				return fmt.Errorf("failed to update job %s: %w (also failed to release lock: %v)", jobID, err, releaseErr)
			}
		}
		return fmt.Errorf("failed to update job %s: %w", jobID, err)
	}

	if job.Action == models.JobActionApply {
		if err := s.releaseApplyLock(ctx, job); err != nil {
			return err
		}
	}

	s.publishResult(ctx, job, success, output, errMsg)
	return nil
}

func (s *jobResultService) FailExpired(ctx context.Context) error {
	expired, err := s.jobs.FailExpiredLeases(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	for i := range expired {
		job := &expired[i]
		if job.Action == models.JobActionApply {
			if err := s.releaseApplyLock(ctx, job); err != nil {
				s.logger.Error("Failed to release lock after lease expiry", "job_id", job.ID, "error", err)
			}
		}
		s.publishResult(ctx, job, false, "", job.ErrorMsg)
	}
	if len(expired) > 0 {
		s.logger.Info("Failed jobs with expired leases", "count", len(expired))
	}
	return nil
}

func (s *jobResultService) releaseApplyLock(ctx context.Context, job *models.Job) error {
	if err := s.locks.Delete(ctx, job.Repo, job.StackName, "default"); err != nil {
		return fmt.Errorf("failed to release lock for job %s stack %q: %w", job.ID, job.StackName, err)
	}
	s.logger.Debug("Released lock for job", "job_id", job.ID, "repo", job.Repo, "stack", job.StackName)
	return nil
}

func (s *jobResultService) publishResult(ctx context.Context, job *models.Job, success bool, output, errMsg string) {
	var comment string
	switch job.Action {
	case models.JobActionPlan:
		comment = feedback.PlanResultComment(job, success, output, errMsg)
	case models.JobActionApply:
		comment = feedback.ApplyResultComment(job, success, output, errMsg)
	default:
		s.logger.Warn("Skipping SCM comment for unknown job action", "job_id", job.ID, "action", job.Action)
		return
	}
	if err := s.scmPublisher.WriteComment(ctx, job.Repo, int(job.PRNumber), comment); err != nil {
		s.logger.Error(
			"Failed to write job result comment",
			"job_id", job.ID,
			"repo", job.Repo,
			"pr", job.PRNumber,
			"stack", job.StackName,
			"action", job.Action,
			"error", err,
		)
	}
}

func NewLeaseReaperFromConfig(logger log.Logger, results JobResultService, cfg *config.Config) *LeaseReaper {
	return NewLeaseReaper(logger, results, cfg.OrchestratorLeaseReaperInterval)
}

// LeaseReaper periodically fails jobs whose leases have expired.
type LeaseReaper struct {
	logger  log.Logger
	results JobResultService
	every   time.Duration
}

func NewLeaseReaper(logger log.Logger, results JobResultService, every time.Duration) *LeaseReaper {
	if every <= 0 {
		every = time.Minute
	}
	return &LeaseReaper{logger: logger, results: results, every: every}
}

func (r *LeaseReaper) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.results.FailExpired(ctx); err != nil {
				r.logger.Error("Failed to reap expired job leases", "error", err)
			}
		}
	}
}
