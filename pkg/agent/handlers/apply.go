package handlers

import (
	"context"
	"fmt"

	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

func (h *Handlers) handleApply(ctx context.Context, jobID string, cmd *terraplanev1.ApplyCommand) {
	h.logger.Info(
		"Running terraplane apply",
		"job_id", jobID,
		"repo", cmd.GetRepo(),
		"pr", cmd.GetPrNumber(),
		"stack", cmd.GetStackName(),
		"dir", cmd.GetDir(),
		"commit", cmd.GetCommitHash(),
	)

	// TODO: clone repo, run terraform apply, capture output
	result := &terraplanev1.ApplyResult{
		Success: true,
		Output:  fmt.Sprintf("stub apply for repository %s pull request #%d", cmd.GetRepo(), cmd.GetPrNumber()),
	}

	if err := h.writeApplyResult(ctx, jobID, result); err != nil {
		h.logger.Error(
			"Failed to send apply result to orchestrator",
			"job_id", jobID,
			"repo", cmd.GetRepo(),
			"error", err,
		)
	}
}
