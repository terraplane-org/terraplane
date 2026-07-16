package handlers

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
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

	workspaceManager := workspace.NewManager(h.logger, h.sshKeyPath, h.workDir)
	workspaceDir, err := workspaceManager.ProvisionWorkspace(ctx, cmd.GetRepo(), cmd.GetCommitHash(), cmd.GetStackName())
	if err != nil {
		h.logger.Error(
			"Failed to provision workspace for terraplane plan",
			"repo", cmd.GetRepo(),
			"pr", cmd.GetPrNumber(),
			"stack", cmd.GetStackName(),
			"error", err,
		)
		if writeErr := h.writePlanResult(ctx, jobID, &terraplanev1.PlanResult{
			Success: false,
			Error:   err.Error(),
		}); writeErr != nil {
			h.logger.Error(
				"Failed to send plan result to orchestrator",
				"job_id", jobID,
				"repo", cmd.GetRepo(),
				"stack", cmd.GetStackName(),
				"error", writeErr,
			)
		}
		return
	}

	defer func() {
		if err != nil {
			if removeErr := workspaceManager.RemoveWorkspace(ctx, workspaceDir); removeErr != nil {
				h.logger.Error(
					"Failed to remove workspace after plan failure",
					"repo", cmd.GetRepo(),
					"pr", cmd.GetPrNumber(),
					"stack", cmd.GetStackName(),
					"error", removeErr,
				)
			}
		}
	}()

	terraformManager := terraform.NewManager(h.logger, workspaceDir, h.terraformBinDir, h.defaultTerraformVersion, jobID)
	output, err := terraformManager.RunPlan(ctx, cmd.GetStackName(), cmd.GetPlanFlags())
	if err != nil {
		h.logger.Error(
			"Failed to run terraform plan",
			"repo", cmd.GetRepo(),
			"pr", cmd.GetPrNumber(),
			"stack", cmd.GetStackName(),
			"error", err,
		)
		if writeErr := h.writePlanResult(ctx, jobID, &terraplanev1.PlanResult{
			Success: false,
			Output:  output,
			Error:   err.Error(),
		}); writeErr != nil {
			h.logger.Error(
				"Failed to send plan result to orchestrator",
				"job_id", jobID,
				"repo", cmd.GetRepo(),
				"stack", cmd.GetStackName(),
				"error", writeErr,
			)
		}
		return
	}

	if writeErr := h.writePlanResult(ctx, jobID, &terraplanev1.PlanResult{
		Success: true,
		Output:  output,
	}); writeErr != nil {
		err = writeErr
		h.logger.Error(
			"Failed to send plan result to orchestrator",
			"job_id", jobID,
			"repo", cmd.GetRepo(),
			"stack", cmd.GetStackName(),
			"error", writeErr,
		)
	}
}
