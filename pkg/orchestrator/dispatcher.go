package orchestrator

import (
	"context"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
)

type Dispatcher interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type dispatcher struct {
	logger          log.Logger
	jobService      services.JobService
	jobPollInterval time.Duration
	unlockService   services.UnlockService
}

func (d *dispatcher) Start(ctx context.Context) error {
	d.logger.Info("Dispatcher started")
	return d.pollForJobs(ctx)
}

func (d *dispatcher) pollForJobs(ctx context.Context) error {
	ticker := time.NewTicker(d.jobPollInterval)
	defer ticker.Stop()

	for {
		d.tick(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (d *dispatcher) tick(ctx context.Context) {
	if err := d.jobService.ReapExpiredClaims(ctx); err != nil {
		d.logger.Error("Failed to reap expired job claims", "error", err)
	}

	commands, err := d.jobService.ClaimPendingJobs(ctx, nil)
	if err != nil {
		d.logger.Error("Failed to claim pending unlock jobs", "error", err)
		return
	}
	for _, cmd := range commands {
		if err := d.dispatchUnlock(ctx, cmd); err != nil {
			d.logger.Error("Failed to dispatch unlock", "error", err)
		}
	}
}

func (d *dispatcher) dispatchUnlock(ctx context.Context, cmd command.Command) error {
	if cmd.Kind != command.KindUnlock {
		d.failClaim(ctx, commandJobID(cmd), "expected unlock job")
		return nil
	}
	if err := d.unlockService.RunUnlock(ctx, cmd.Unlock); err != nil {
		d.failClaim(ctx, cmd.Unlock.JobID, err.Error())
		return err
	}
	return nil
}

func (d *dispatcher) failClaim(ctx context.Context, jobID, errMsg string) {
	if jobID == "" {
		return
	}
	if err := d.jobService.FailClaimedJob(ctx, jobID, errMsg); err != nil {
		d.logger.Error("Failed to mark job failed", "job_id", jobID, "error", err)
	}
}

func commandJobID(cmd command.Command) string {
	switch cmd.Kind {
	case command.KindPlan:
		return cmd.Plan.JobID
	case command.KindApply:
		return cmd.Apply.JobID
	case command.KindUnlock:
		return cmd.Unlock.JobID
	default:
		return ""
	}
}

func (d *dispatcher) Shutdown(ctx context.Context) error {
	d.logger.Debug("Dispatcher shutdown")
	return nil
}

func NewDispatcher(config *config.Config, logger log.Logger, jobService services.JobService, unlockService services.UnlockService) Dispatcher {
	return &dispatcher{
		logger:          logger,
		jobService:      jobService,
		jobPollInterval: config.OrchestratorDispatcherJobPollInterval,
		unlockService:   unlockService,
	}
}
