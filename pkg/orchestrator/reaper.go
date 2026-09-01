package orchestrator

import (
	"context"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
)

type Dispatcher interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type dispatcher struct {
	logger                 log.Logger
	jobService             services.JobService
	jobPollInterval        time.Duration
	jobCleanupPollInterval time.Duration
}

func (d *dispatcher) Start(ctx context.Context) error {
	d.logger.Info("Reaper started")
	return d.run(ctx)
}

func (d *dispatcher) run(ctx context.Context) error {
	reapTicker := time.NewTicker(d.jobPollInterval)
	defer reapTicker.Stop()

	d.reapExpiredClaims(ctx)

	var cleanupC <-chan time.Time
	if d.jobCleanupPollInterval > 0 {
		cleanupTicker := time.NewTicker(d.jobCleanupPollInterval)
		defer cleanupTicker.Stop()
		cleanupC = cleanupTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-reapTicker.C:
			d.reapExpiredClaims(ctx)
		case <-cleanupC:
			d.cleanupExpiredJobs(ctx)
		}
	}
}

func (d *dispatcher) reapExpiredClaims(ctx context.Context) {
	if err := d.jobService.ReapExpiredClaims(ctx); err != nil {
		d.logger.Error("Failed to reap expired job claims", "error", err)
	}
}

func (d *dispatcher) cleanupExpiredJobs(ctx context.Context) {
	if err := d.jobService.CleanupExpiredJobs(ctx); err != nil {
		d.logger.Error("Failed to cleanup expired jobs", "error", err)
	}
}

func (d *dispatcher) Shutdown(ctx context.Context) error {
	d.logger.Debug("Reaper shutdown")
	return nil
}

func NewDispatcher(config *config.Config, logger log.Logger, jobService services.JobService) Dispatcher {
	return &dispatcher{
		logger:                 logger,
		jobService:             jobService,
		jobPollInterval:        config.OrchestratorDispatcherJobPollInterval,
		jobCleanupPollInterval: config.OrchestratorJobCleanupPollInterval,
	}
}
