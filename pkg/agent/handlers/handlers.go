package handlers

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type WriteFunc func(ctx context.Context, env *terraplanev1.TerraformEnvelope) error

type Handlers struct {
	logger           log.Logger
	write            WriteFunc
	workspaceManager workspace.Manager
	terraformManager terraform.Manager
}

func New(logger log.Logger, write WriteFunc, workspaceManager workspace.Manager, terraformManager terraform.Manager) *Handlers {
	return &Handlers{
		logger:           logger,
		write:            write,
		workspaceManager: workspaceManager,
		terraformManager: terraformManager,
	}
}

func (h *Handlers) accept(ctx context.Context, jobID, ackMessage string, run func(context.Context)) error {
	if err := h.writeAck(ctx, jobID, ackMessage); err != nil {
		return err
	}
	run(ctx)
	return nil
}

func (h *Handlers) Dispatch(ctx context.Context, env *terraplanev1.TerraformEnvelope) error {
	switch p := env.GetPayload().(type) {
	case *terraplanev1.TerraformEnvelope_Plan:
		return h.accept(ctx, env.GetJobId(), "plan accepted", func(ctx context.Context) {
			h.handlePlan(ctx, env.GetJobId(), p.Plan)
		})
	case *terraplanev1.TerraformEnvelope_Apply:
		return h.accept(ctx, env.GetJobId(), "apply accepted", func(ctx context.Context) {
			h.handleApply(ctx, env.GetJobId(), p.Apply)
		})
	case *terraplanev1.TerraformEnvelope_Unlock:
		return h.accept(ctx, env.GetJobId(), "unlock accepted", func(ctx context.Context) {
			h.handleUnlock(ctx, env.GetJobId(), p.Unlock)
		})
	default:
		h.logger.Warn("Received unsupported terraform envelope payload", "job_id", env.GetJobId())
		return nil
	}
}
