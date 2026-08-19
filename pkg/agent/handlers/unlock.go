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
	output := fmt.Sprintf("stub unlock for repository %s pull request #%d", cmd.GetRepo(), cmd.GetPrNumber())

	if err := h.orchestratorClient.SubmitResult(ctx, jobID, h.agentID, true, output, ""); err != nil {
		h.logger.Error(
			"Failed to submit unlock result to orchestrator",
			"job_id", jobID,
			"agent_id", h.agentID,
			"repo", cmd.GetRepo(),
			"error", err,
		)
	}
}
