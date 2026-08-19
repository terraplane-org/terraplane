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
	logger          log.Logger
	jobService      services.JobService
	jobPollInterval time.Duration
}

func (d *dispatcher) Start(ctx context.Context) error {
	d.logger.Info("Reaper started")
	return d.run(ctx)
}

func (d *dispatcher) run(ctx context.Context) error {
	ticker := time.NewTicker(d.jobPollInterval)
	defer ticker.Stop()

	for {
		if err := d.jobService.ReapExpiredClaims(ctx); err != nil {
			d.logger.Error("Failed to reap expired job claims", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (d *dispatcher) Shutdown(ctx context.Context) error {
	d.logger.Debug("Reaper shutdown")
	return nil
}

func NewDispatcher(config *config.Config, logger log.Logger, jobService services.JobService) Dispatcher {
	return &dispatcher{
		logger:          logger,
		jobService:      jobService,
		jobPollInterval: config.OrchestratorDispatcherJobPollInterval,
	}
}
