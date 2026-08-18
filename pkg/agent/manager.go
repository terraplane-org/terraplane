package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/handlers"
	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
	"github.com/xyzjace/terraplane/pkg/agentapi"
	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type Manager interface {
	Start(ctx context.Context) error
}

type manager struct {
	logger           log.Logger
	id               string
	client           *orchClient
	pollInterval     time.Duration
	heartbeatEvery   time.Duration
	workspaceManager workspace.Manager
	terraformManager terraform.Manager
}

func (o *manager) Start(ctx context.Context) error {
	if o.id == "" {
		o.logger.Error("Agent ID is not set. Please set the AGENT_ID environment variable.")
		return fmt.Errorf("agent ID is not set")
	}
	if o.client.token == "" {
		o.logger.Error("Shared auth token is not set. Please set the SHARED_AUTH_TOKEN environment variable.")
		return fmt.Errorf("shared auth token is not set")
	}

	o.logger.Info("Starting agent...", "orchestrator", o.client.baseURL)
	h := handlers.New(o.logger, o.client.Write, o.workspaceManager, o.terraformManager)

	for {
		worked, err := o.tick(ctx, h)
		if err != nil {
			if ctx.Err() != nil {
				o.logger.Info("Agent stopped")
				return nil
			}
			o.logger.Warn("Agent poll failed", "error", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			o.logger.Info("Agent stopped")
			return nil
		case <-time.After(o.pollInterval):
		}
	}
}

func (o *manager) tick(ctx context.Context, h *handlers.Handlers) (bool, error) {
	job, err := o.client.Poll(ctx)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}

	o.logger.Info("Claimed job", "job_id", job.JobID, "action", job.Action, "stack", job.StackName)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go o.heartbeat(runCtx, job.JobID)

	env, err := envelopeFromJob(job)
	if err != nil {
		_ = o.client.Result(ctx, job.JobID, agentapi.Result{Success: false, Error: err.Error()})
		return true, err
	}
	return true, h.Dispatch(runCtx, env)
}

func (o *manager) heartbeat(ctx context.Context, jobID string) {
	ticker := time.NewTicker(o.heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := o.client.Heartbeat(ctx, jobID); err != nil && ctx.Err() == nil {
				o.logger.Warn("Job heartbeat failed", "job_id", jobID, "error", err)
			}
		}
	}
}

func envelopeFromJob(job *agentapi.Job) (*terraplanev1.TerraformEnvelope, error) {
	switch job.Action {
	case "plan":
		return &terraplanev1.TerraformEnvelope{
			JobId: job.JobID,
			Payload: &terraplanev1.TerraformEnvelope_Plan{
				Plan: &terraplanev1.PlanCommand{
					Repo:       job.Repo,
					PrNumber:   job.PRNumber,
					CommitHash: job.CommitSHA,
					PlanFlags:  job.PlanFlags,
					StackName:  job.StackName,
					Dir:        job.Dir,
				},
			},
		}, nil
	case "apply":
		return &terraplanev1.TerraformEnvelope{
			JobId: job.JobID,
			Payload: &terraplanev1.TerraformEnvelope_Apply{
				Apply: &terraplanev1.ApplyCommand{
					Repo:       job.Repo,
					PrNumber:   job.PRNumber,
					CommitHash: job.CommitSHA,
					StackName:  job.StackName,
					Dir:        job.Dir,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported job action %q", job.Action)
	}
}

func NewManager(
	config *config.Config,
	logger log.Logger,
	workspaceManager workspace.Manager,
	terraformManager terraform.Manager,
) Manager {
	heartbeat := config.AgentHeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = config.OrchestratorJobLease / 3
	}
	if heartbeat <= 0 {
		heartbeat = 30 * time.Second
	}
	return &manager{
		id:               config.AgentID,
		logger:           logger,
		client:           newOrchClient(config.AgentOrchestratorURL, config.AgentID, config.SharedAuthToken),
		pollInterval:     config.AgentJobPollInterval,
		heartbeatEvery:   heartbeat,
		workspaceManager: workspaceManager,
		terraformManager: terraformManager,
	}
}
