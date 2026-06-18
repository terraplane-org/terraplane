package handlers

import (
	"context"

	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

func (h *Handlers) writeAck(ctx context.Context, jobID, message string) error {
	return h.write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: jobID,
		Payload: &terraplanev1.TerraformEnvelope_Ack{
			Ack: &terraplanev1.Ack{Message: message},
		},
	})
}

func (h *Handlers) writePlanResult(ctx context.Context, jobID string, result *terraplanev1.PlanResult) error {
	return h.write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: jobID,
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: result,
		},
	})
}

func (h *Handlers) writeApplyResult(ctx context.Context, jobID string, result *terraplanev1.ApplyResult) error {
	return h.write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: jobID,
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: result,
		},
	})
}

func (h *Handlers) writeUnlockResult(ctx context.Context, jobID string, result *terraplanev1.UnlockResult) error {
	return h.write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: jobID,
		Payload: &terraplanev1.TerraformEnvelope_UnlockResult{
			UnlockResult: result,
		},
	})
}
