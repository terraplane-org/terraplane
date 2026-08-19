package handlers

import (
	"context"

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

	workspaceDir, err := h.workspaceManager.FetchWorkspace(ctx, cmd.GetRepo(), cmd.GetCommitHash(), cmd.GetStackName())
	if err != nil {
		h.logger.Error(
			"Failed to fetch workspace for terraplane apply",
			"repo", cmd.GetRepo(),
			"pr", cmd.GetPrNumber(),
			"stack", cmd.GetStackName(),
			"error", err,
		)
		if writeErr := h.orchestratorClient.SubmitResult(ctx, jobID, h.agentID, false, "", err.Error()); writeErr != nil {
			h.logger.Error(
				"Failed to submit apply result to orchestrator",
				"job_id", jobID,
				"agent_id", h.agentID,
				"error", writeErr,
			)
		}
		return
	}

	defer func() {
		if removeErr := h.workspaceManager.RemoveWorkspace(ctx, workspaceDir); removeErr != nil {
			h.logger.Error(
				"Failed to remove workspace after apply",
				"repo", cmd.GetRepo(),
				"pr", cmd.GetPrNumber(),
				"stack", cmd.GetStackName(),
				"error", removeErr,
			)
		}
	}()

	output, err := h.terraformManager.RunApply(ctx, workspaceDir, cmd.GetStackName())
	if err != nil {
		h.logger.Error(
			"Failed to run terraform apply",
			"repo", cmd.GetRepo(),
			"pr", cmd.GetPrNumber(),
			"stack", cmd.GetStackName(),
			"error", err,
		)
		if writeErr := h.orchestratorClient.SubmitResult(ctx, jobID, h.agentID, false, output, err.Error()); writeErr != nil {
			h.logger.Error(
				"Failed to submit apply result to orchestrator",
				"job_id", jobID,
				"agent_id", h.agentID,
				"error", writeErr,
			)
		}
		return
	}

	if writeErr := h.orchestratorClient.SubmitResult(ctx, jobID, h.agentID, true, output, ""); writeErr != nil {
		h.logger.Error(
			"Failed to submit apply result to orchestrator",
			"job_id", jobID,
			"repo", cmd.GetRepo(),
			"stack", cmd.GetStackName(),
			"error", writeErr,
		)
	}
}
