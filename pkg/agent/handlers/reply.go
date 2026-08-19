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
