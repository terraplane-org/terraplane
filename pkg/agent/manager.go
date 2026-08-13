package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/handlers"
	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

const idleSleep = 5 * time.Second

type Manager interface {
	Start(ctx context.Context) error
}

type manager struct {
	logger           log.Logger
	id               string
	client           *orchestratorClient
	idleSleep        time.Duration
	workspaceManager workspace.Manager
	terraformManager terraform.Manager
}

func NewManager(
	cfg *config.Config,
	logger log.Logger,
	workspaceManager workspace.Manager,
	terraformManager terraform.Manager,
) Manager {
	baseURL := normalizeOrchestratorURL(cfg.AgentOrchestratorURL)
	claimWait := cfg.AgentClaimWait
	if claimWait <= 0 {
		claimWait = 30 * time.Second
	}
	return &manager{
		id:               cfg.AgentID,
		logger:           logger,
		client:           newOrchestratorClient(baseURL, cfg.AgentID, cfg.SharedAuthToken, claimWait),
		idleSleep:        idleSleep,
		workspaceManager: workspaceManager,
		terraformManager: terraformManager,
	}
}

func normalizeOrchestratorURL(raw string) string {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "ws://"):
		raw = "http://" + strings.TrimPrefix(raw, "ws://")
	case strings.HasPrefix(raw, "wss://"):
		raw = "https://" + strings.TrimPrefix(raw, "wss://")
	}
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, "/ws")
	return strings.TrimRight(raw, "/")
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

	o.logger.Info("Starting agent...", "orchestrator_url", o.client.baseURL, "agent_id", o.id)

	write := func(ctx context.Context, env *terraplanev1.TerraformEnvelope) error {
		switch p := env.GetPayload().(type) {
		case *terraplanev1.TerraformEnvelope_PlanResult:
			return o.client.ReportResult(ctx, env.GetJobId(), p.PlanResult.GetSuccess(), p.PlanResult.GetOutput(), p.PlanResult.GetError())
		case *terraplanev1.TerraformEnvelope_ApplyResult:
			return o.client.ReportResult(ctx, env.GetJobId(), p.ApplyResult.GetSuccess(), p.ApplyResult.GetOutput(), p.ApplyResult.GetError())
		case *terraplanev1.TerraformEnvelope_Ack:
			return nil
		default:
			o.logger.Debug("Ignoring unsupported write payload", "job_id", env.GetJobId())
			return nil
		}
	}
	h := handlers.New(o.logger, write, o.workspaceManager, o.terraformManager)

	for {
		if ctx.Err() != nil {
			o.logger.Info("Agent stopped")
			return nil
		}

		job, err := o.client.Claim(ctx)
		if ctx.Err() != nil {
			o.logger.Info("Agent stopped")
			return nil
		}
		if err != nil {
			o.logger.Warn("Failed to claim job; retrying", "error", err, "after", o.idleSleep)
			if err := sleep(ctx, o.idleSleep); err != nil {
				o.logger.Info("Agent stopped")
				return nil
			}
			continue
		}
		if job == nil {
			if err := sleep(ctx, o.idleSleep); err != nil {
				o.logger.Info("Agent stopped")
				return nil
			}
			continue
		}

		o.logger.Info(
			"Claimed job",
			"job_id", job.ID,
			"action", job.Action,
			"repo", job.Repo,
			"stack", job.StackName,
		)

		if err := o.dispatch(ctx, h, job); err != nil {
			o.logger.Error("Failed to dispatch claimed job", "job_id", job.ID, "error", err)
			_ = o.client.ReportResult(ctx, job.ID, false, "", err.Error())
		}
	}
}

func (o *manager) dispatch(ctx context.Context, h *handlers.Handlers, job *claimedJob) error {
	switch job.action() {
	case models.JobActionPlan:
		return h.Dispatch(ctx, &terraplanev1.TerraformEnvelope{
			JobId: job.ID,
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
		})
	case models.JobActionApply:
		return h.Dispatch(ctx, &terraplanev1.TerraformEnvelope{
			JobId: job.ID,
			Payload: &terraplanev1.TerraformEnvelope_Apply{
				Apply: &terraplanev1.ApplyCommand{
					Repo:       job.Repo,
					PrNumber:   job.PRNumber,
					CommitHash: job.CommitSHA,
					StackName:  job.StackName,
					Dir:        job.Dir,
				},
			},
		})
	default:
		return fmt.Errorf("unsupported job action %q", job.Action)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
