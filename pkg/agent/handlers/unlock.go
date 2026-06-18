package handlers

import (
	"context"
	"fmt"

	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

func (h *Handlers) handleUnlock(ctx context.Context, jobID string, cmd *terraplanev1.UnlockCommand) {
	h.logger.Info(
		"Running terraplane unlock",
		"job_id", jobID,
		"repo", cmd.GetRepo(),
		"pr", cmd.GetPrNumber(),
	)

	// TODO: release terraform state lock
	result := &terraplanev1.UnlockResult{
		Success: true,
		Output:  fmt.Sprintf("stub unlock for repository %s pull request #%d", cmd.GetRepo(), cmd.GetPrNumber()),
	}

	if err := h.writeUnlockResult(ctx, jobID, result); err != nil {
		h.logger.Error(
			"Failed to send unlock result to orchestrator",
			"job_id", jobID,
			"repo", cmd.GetRepo(),
			"error", err,
		)
	}
}
