package handlers

import (
	"context"
	"fmt"

	"github.com/xyzjace/terraplane/pkg/command"
)

func (h *Handlers) handleUnlock(ctx context.Context, cmd *command.UnlockCommand) {
	jobID := cmd.JobID
	h.logger.Info(
		"Running terraplane unlock",
		"job_id", jobID,
		"repo", cmd.Repo,
		"pr", cmd.PRNumber,
	)

	// TODO: release terraform state lock
	output := fmt.Sprintf("stub unlock for repository %s pull request #%d", cmd.Repo, cmd.PRNumber)

	if err := h.orchestratorClient.SubmitResult(ctx, jobID, h.agentID, true, output, ""); err != nil {
		h.logger.Error(
			"Failed to submit unlock result to orchestrator",
			"job_id", jobID,
			"agent_id", h.agentID,
			"repo", cmd.Repo,
			"error", err,
		)
	}
}
