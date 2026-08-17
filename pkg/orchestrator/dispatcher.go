package orchestrator

import (
	"context"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type Dispatcher interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type dispatcher struct {
	logger          log.Logger
	jobService      services.JobService
	scmProvider     scm.Provider
	sessionRegistry agentsession.Registry
	jobPollInterval time.Duration
}

func (d *dispatcher) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		d.logger.Info("Dispatcher started")
		errCh <- d.pollForJobs(ctx)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return nil
	}
}

func (d *dispatcher) pollForJobs(ctx context.Context) error {
	for {
		agents := d.sessionRegistry.GetAllAgents()
		if len(agents) > 0 {
			jobs, err := d.jobService.ClaimPendingJobs(ctx, agents)
			if err != nil {
				d.logger.Error("Failed to claim pending jobs", "error", err)
			} else {
				for _, job := range jobs {
					d.logger.Info("Dispatching job to agent", "job", job, "agent", job.AgentID)
				}
			}
		}
		time.Sleep(d.jobPollInterval)
	}
}

func (d *dispatcher) Shutdown(ctx context.Context) error {
	d.logger.Debug("Dispatcher shutdown")
	return nil
}

func NewDispatcher(config *config.Config, logger log.Logger, jobService services.JobService, scmProvider scm.Provider, sessionRegistry agentsession.Registry) Dispatcher {
	return &dispatcher{
		logger:          logger,
		jobService:      jobService,
		scmProvider:     scmProvider,
		sessionRegistry: sessionRegistry,
		jobPollInterval: config.OrchestratorDispatcherJobPollInterval,
	}
}
