package handlers

import (
	"context"
	"time"

	"github.com/xyzjace/terraplane/internal/process"
)

func (h *Handlers) streamProgress(ctx context.Context, jobID string, buf *process.StreamBuffer) func() {
	if h.progressInterval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(h.progressInterval)
		defer ticker.Stop()

		var last *string
		report := func() {
			snap := buf.Snapshot()
			if last != nil && snap == *last {
				return
			}
			last = &snap
			if err := h.orchestratorClient.SubmitProgress(ctx, jobID, h.agentID, snap); err != nil {
				h.logger.Warn(
					"Failed to submit job progress",
					"job_id", jobID,
					"agent_id", h.agentID,
					"error", err,
				)
			}
		}

		report()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report()
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}
