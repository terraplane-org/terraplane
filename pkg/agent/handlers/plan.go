package handlers

import (
	"context"
	"fmt"

	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

func (h *Handlers) handlePlan(ctx context.Context, jobID string, cmd *terraplanev1.PlanCommand) {
	h.logger.Info(
		"Running terraplane plan",
		"job_id", jobID,
		"repo", cmd.GetRepo(),
		"pr", cmd.GetPrNumber(),
		"stack", cmd.GetStackName(),
		"dir", cmd.GetDir(),
		"commit", cmd.GetCommitHash(),
		"plan_flags", cmd.GetPlanFlags(),
	)

	// TODO: clone repo, run terraform plan, capture output
	result := &terraplanev1.PlanResult{
		Success: true,
		Output:  fmt.Sprintf("stub plan for stack %q in %q", cmd.GetStackName(), cmd.GetDir()),
	}

	if err := h.writePlanResult(ctx, jobID, result); err != nil {
		h.logger.Error(
			"Failed to send plan result to orchestrator",
			"job_id", jobID,
			"repo", cmd.GetRepo(),
			"stack", cmd.GetStackName(),
			"error", err,
		)
	}
}
