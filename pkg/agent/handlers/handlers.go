package handlers

import (
	"context"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/orchestrator"
	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
)

type Handlers struct {
	logger             log.Logger
	workspaceManager   workspace.Manager
	terraformManager   terraform.Manager
	orchestratorClient orchestrator.Client
	agentID            string
}

func New(logger log.Logger, config *config.Config, workspaceManager workspace.Manager, terraformManager terraform.Manager, orchestratorClient orchestrator.Client) *Handlers {
	return &Handlers{
		logger:             logger,
		agentID:            config.AgentID,
		workspaceManager:   workspaceManager,
		terraformManager:   terraformManager,
		orchestratorClient: orchestratorClient,
	}
}

func (h *Handlers) Dispatch(ctx context.Context, cmd *command.Command) {
	go func() {
		ctx = context.WithoutCancel(ctx)
		switch cmd.Kind {
		case command.KindPlan:
			h.handlePlan(ctx, &cmd.Plan)
		case command.KindApply:
			h.handleApply(ctx, &cmd.Apply)
		case command.KindUnlock:
			h.handleUnlock(ctx, &cmd.Unlock)
		default:
			h.logger.Warn("Received unsupported command kind", "kind", cmd.Kind, "job_id", cmd.Plan.JobID)
		}
	}()
}
