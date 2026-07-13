package handlers

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
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

	workspaceManager := workspace.NewManager(h.logger, h.sshKeyPath, h.workDir)
	workspaceDir, err := workspaceManager.FetchWorkspace(ctx, cmd.GetRepo(), cmd.GetCommitHash(), cmd.GetStackName())
	if err != nil {
		h.logger.Error(
			"Failed to fetch workspace for terraplane apply",
			"repo", cmd.GetRepo(),
			"pr", cmd.GetPrNumber(),
			"stack", cmd.GetStackName(),
			"error", err,
		)
		if writeErr := h.writeApplyResult(ctx, jobID, &terraplanev1.ApplyResult{
			Success: false,
			Error:   err.Error(),
		}); writeErr != nil {
			h.logger.Error(
				"Failed to send apply result to orchestrator",
				"job_id", jobID,
				"repo", cmd.GetRepo(),
				"stack", cmd.GetStackName(),
				"error", writeErr,
			)
		}
		return
	}

	defer func() {
		if removeErr := workspaceManager.RemoveWorkspace(ctx); removeErr != nil {
			h.logger.Error(
				"Failed to remove workspace after apply",
				"repo", cmd.GetRepo(),
				"pr", cmd.GetPrNumber(),
				"stack", cmd.GetStackName(),
				"error", removeErr,
			)
		}
	}()

	terraformManager := terraform.NewManager(h.logger, workspaceDir, h.terraformBinDir, h.defaultTerraformVersion, jobID)
	output, err := terraformManager.RunApply(ctx, cmd.GetStackName())
	if err != nil {
		h.logger.Error(
			"Failed to run terraform apply",
			"repo", cmd.GetRepo(),
			"pr", cmd.GetPrNumber(),
			"stack", cmd.GetStackName(),
			"error", err,
		)
		if writeErr := h.writeApplyResult(ctx, jobID, &terraplanev1.ApplyResult{
			Success: false,
			Output:  output,
			Error:   err.Error(),
		}); writeErr != nil {
			h.logger.Error(
				"Failed to send apply result to orchestrator",
				"job_id", jobID,
				"repo", cmd.GetRepo(),
				"stack", cmd.GetStackName(),
				"error", writeErr,
			)
		}
		return
	}

	// TODO: Output here is going to be giant, this is not a smart way of doing things
	if writeErr := h.writeApplyResult(ctx, jobID, &terraplanev1.ApplyResult{
		Success: true,
		Output:  output,
	}); writeErr != nil {
		h.logger.Error(
			"Failed to send apply result to orchestrator",
			"job_id", jobID,
			"repo", cmd.GetRepo(),
			"stack", cmd.GetStackName(),
			"error", writeErr,
		)
	}
}
