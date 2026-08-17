package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
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
	scmPublisher    scm.Publisher
	sessionRegistry agentsession.Registry
	jobPollInterval time.Duration
	planService     services.PlanService
	applyService    services.ApplyService
	unlockService   services.UnlockService
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
			commands, err := d.jobService.ClaimPendingJobs(ctx, agents)
			if err != nil {
				d.logger.Error("Failed to claim pending jobs", "error", err)
			} else {
				for _, cmd := range commands {
					if err := d.dispatchJob(ctx, cmd); err != nil {
						d.logger.Error("Failed to dispatch job", "kind", cmd.Kind, "error", err)
					}
				}
			}
		}
		time.Sleep(d.jobPollInterval)
	}
}

func (d *dispatcher) dispatchJob(ctx context.Context, cmd command.Command) error {
	var (
		agentID string
		run     func() error
	)
	switch cmd.Kind {
	case command.KindPlan:
		agentID = cmd.Plan.Agent
		run = func() error { return d.planService.RunPlan(ctx, cmd.Plan) }
	case command.KindApply:
		agentID = cmd.Apply.Agent
		run = func() error { return d.applyService.RunApply(ctx, cmd.Apply) }
	case command.KindUnlock:
		agentID = cmd.Unlock.Agent
		run = func() error { return d.unlockService.RunUnlock(ctx, cmd.Unlock) }
	default:
		return fmt.Errorf("unknown command kind: %s", cmd.Kind)
	}

	session, err := d.sessionRegistry.Get(ctx, agentID)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("agent %s is not connected", agentID)
	}

	d.logger.Info("Dispatching job", "agent", session.ID(), "kind", cmd.Kind)
	return run()
}

func (d *dispatcher) Shutdown(ctx context.Context) error {
	d.logger.Debug("Dispatcher shutdown")
	return nil
}

func NewDispatcher(config *config.Config, logger log.Logger, jobService services.JobService, scmPublisher scm.Publisher, sessionRegistry agentsession.Registry, planService services.PlanService, applyService services.ApplyService, unlockService services.UnlockService) Dispatcher {
	return &dispatcher{
		logger:          logger,
		jobService:      jobService,
		scmPublisher:    scmPublisher,
		sessionRegistry: sessionRegistry,
		jobPollInterval: config.OrchestratorDispatcherJobPollInterval,
		planService:     planService,
		applyService:    applyService,
		unlockService:   unlockService,
	}
}
