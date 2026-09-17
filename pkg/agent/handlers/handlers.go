package handlers

import (
	"context"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/process"
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
	periodicInterval   time.Duration
}

func New(logger log.Logger, config *config.Config, workspaceManager workspace.Manager, terraformManager terraform.Manager, orchestratorClient orchestrator.Client) *Handlers {
	return &Handlers{
		logger:             logger,
		agentID:            config.AgentID,
		periodicInterval:   config.AgentPeriodicCommentInterval,
		workspaceManager:   workspaceManager,
		terraformManager:   terraformManager,
		orchestratorClient: orchestratorClient,
	}
}

// Dispatch runs the command in a background goroutine and closes done when finished.
// Callers that need to wait for completion (e.g. to keep a heartbeat alive) should
// pass a non-nil done channel and block on it.
func (h *Handlers) Dispatch(ctx context.Context, cmd *command.Command, done chan<- struct{}) {
	go func() {
		if done != nil {
			defer close(done)
		}
		ctx = context.WithoutCancel(ctx)
		switch cmd.Kind {
		case command.KindPlan:
			h.handlePlan(ctx, &cmd.Plan)
		case command.KindApply:
			h.handleApply(ctx, &cmd.Apply)
		default:
			h.logger.Warn("Received unsupported command kind", "kind", cmd.Kind)
		}
	}()
}

func (h *Handlers) watchOutput(ctx context.Context, jobID string, out *process.Buffer) func() {
	if h.periodicInterval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(h.periodicInterval)
		defer ticker.Stop()
		var last string
		for {
			select {
			case <-ticker.C:
				snapshot := out.String()
				if snapshot == "" || snapshot == last {
					continue
				}
				if err := h.orchestratorClient.SubmitPeriodicResult(ctx, jobID, h.agentID, snapshot); err != nil {
					h.logger.Warn("Failed to submit periodic result", "job_id", jobID, "agent_id", h.agentID, "error", err)
					continue
				}
				last = snapshot
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
