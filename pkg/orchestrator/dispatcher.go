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
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type Dispatcher interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type dispatcher struct {
	logger          log.Logger
	jobService      services.JobService
	sessionRegistry agentsession.Registry
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

	agents := d.sessionRegistry.GetAllAgents()
	d.claimAndDispatch(ctx, "")
	for _, agentID := range agents {
		d.claimAndDispatch(ctx, agentID)
	}
}

func (d *dispatcher) claimAndDispatch(ctx context.Context, agentID string) {
	cmd, err := d.jobService.ClaimPendingJob(ctx, agentID)
	if err != nil {
		d.logger.Error("Failed to claim pending job", "agent", agentID, "error", err)
		return
	}
	if cmd == nil {
		return
	}
	if err := d.dispatchJob(ctx, *cmd); err != nil {
		d.logger.Error("Failed to dispatch job", "agent", agentID, "kind", cmd.Kind, "error", err)
	}
}

func (d *dispatcher) dispatchJob(ctx context.Context, cmd command.Command) error {
	jobID := commandJobID(cmd)

	switch cmd.Kind {
	case command.KindUnlock:
		if err := d.unlockService.RunUnlock(ctx, cmd.Unlock); err != nil {
			d.failClaim(ctx, jobID, err.Error())
			return err
		}
		return nil
	case command.KindPlan, command.KindApply:
		envelope, err := terraformEnvelope(cmd)
		if err != nil {
			d.failClaim(ctx, jobID, err.Error())
			return err
		}

		session, err := d.sessionRegistry.Get(ctx, commandAgent(cmd))
		if err != nil {
			d.releaseClaim(ctx, jobID)
			return err
		}
		if session == nil {
			d.releaseClaim(ctx, jobID)
			return fmt.Errorf("agent %s is not connected", commandAgent(cmd))
		}

		d.logger.Info("Dispatching job", "agent", session.ID(), "kind", cmd.Kind, "job_id", jobID)
		if err := session.Write(ctx, envelope); err != nil {
			d.releaseClaim(ctx, jobID)
			return err
		}
		return nil
	default:
		d.failClaim(ctx, jobID, fmt.Sprintf("unknown command kind: %s", cmd.Kind))
		return fmt.Errorf("unknown command kind: %s", cmd.Kind)
	}
}

func (d *dispatcher) releaseClaim(ctx context.Context, jobID string) {
	if jobID == "" {
		return
	}
	if err := d.jobService.ReleaseClaim(ctx, jobID); err != nil {
		d.logger.Error("Failed to release job claim", "job_id", jobID, "error", err)
	}
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

func commandAgent(cmd command.Command) string {
	switch cmd.Kind {
	case command.KindPlan:
		return cmd.Plan.Agent
	case command.KindApply:
		return cmd.Apply.Agent
	case command.KindUnlock:
		return cmd.Unlock.Agent
	default:
		return ""
	}
}

func terraformEnvelope(cmd command.Command) (*terraplanev1.TerraformEnvelope, error) {
	switch cmd.Kind {
	case command.KindPlan:
		stack := firstStack(cmd.Plan.Stacks)
		if cmd.Plan.JobID == "" || stack == "" {
			return nil, fmt.Errorf("plan command missing job id or stack")
		}
		return &terraplanev1.TerraformEnvelope{
			JobId: cmd.Plan.JobID,
			Payload: &terraplanev1.TerraformEnvelope_Plan{
				Plan: &terraplanev1.PlanCommand{
					Repo:       cmd.Plan.Repo,
					PrNumber:   int32(cmd.Plan.PRNumber),
					CommitHash: cmd.Plan.CommitSHA,
					PlanFlags:  cmd.Plan.PlanFlags,
					StackName:  stack,
					Dir:        cmd.Plan.Dir,
				},
			},
		}, nil
	case command.KindApply:
		stack := firstStack(cmd.Apply.Stacks)
		if cmd.Apply.JobID == "" || stack == "" {
			return nil, fmt.Errorf("apply command missing job id or stack")
		}
		return &terraplanev1.TerraformEnvelope{
			JobId: cmd.Apply.JobID,
			Payload: &terraplanev1.TerraformEnvelope_Apply{
				Apply: &terraplanev1.ApplyCommand{
					Repo:       cmd.Apply.Repo,
					PrNumber:   int32(cmd.Apply.PRNumber),
					CommitHash: cmd.Apply.CommitSHA,
					StackName:  stack,
					Dir:        cmd.Apply.Dir,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown command kind: %s", cmd.Kind)
	}
}

func firstStack(stacks []string) string {
	if len(stacks) == 0 {
		return ""
	}
	return stacks[0]
}

func (d *dispatcher) Shutdown(ctx context.Context) error {
	d.logger.Debug("Dispatcher shutdown")
	return nil
}

func NewDispatcher(config *config.Config, logger log.Logger, jobService services.JobService, sessionRegistry agentsession.Registry, unlockService services.UnlockService) Dispatcher {
	return &dispatcher{
		logger:          logger,
		jobService:      jobService,
		sessionRegistry: sessionRegistry,
		jobPollInterval: config.OrchestratorDispatcherJobPollInterval,
		unlockService:   unlockService,
	}
}
