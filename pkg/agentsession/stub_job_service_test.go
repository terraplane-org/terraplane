package agentsession

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type noopJobService struct{}

func (noopJobService) CreatePendingJobs(context.Context, *scm.Webhook) error { return nil }
func (noopJobService) ClaimPendingJob(context.Context, string) (*command.Command, error) {
	return nil, nil
}
func (noopJobService) ReleaseClaim(context.Context, string) error           { return nil }
func (noopJobService) FailClaimedJob(context.Context, string, string) error { return nil }
func (noopJobService) ReapExpiredClaims(context.Context) error              { return nil }
func (noopJobService) RefreshAgentClaims(context.Context, string) error     { return nil }
func (noopJobService) AckJob(context.Context, string) error                 { return nil }
func (noopJobService) CommitJobResult(context.Context, string, string, string, string) error {
	return nil
}

var _ services.JobService = noopJobService{}

type signalAckJobService struct {
	noopJobService
	err  error
	done chan struct{}
}

func (s *signalAckJobService) AckJob(context.Context, string) error {
	if s.done != nil {
		close(s.done)
	}
	return s.err
}

var _ services.JobService = (*signalAckJobService)(nil)
