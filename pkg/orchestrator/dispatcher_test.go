package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type reaperJobs struct {
	reaped  int
	reapErr error
}

func (s *reaperJobs) CreatePendingJobs(context.Context, *scm.Webhook) error { return nil }
func (s *reaperJobs) ClaimPendingJob(context.Context, string) (*command.Command, error) {
	return nil, nil
}
func (s *reaperJobs) ReleaseClaim(context.Context, string) error           { return nil }
func (s *reaperJobs) FailClaimedJob(context.Context, string, string) error { return nil }
func (s *reaperJobs) RefreshAgentClaims(context.Context, string) error     { return nil }
func (s *reaperJobs) AckJob(context.Context, string, string) error         { return nil }
func (s *reaperJobs) CommitJobResult(context.Context, string, string, string, string, string) error {
	return nil
}
func (s *reaperJobs) ReapExpiredClaims(context.Context) error {
	s.reaped++
	return s.reapErr
}

func startAndCancel(t *testing.T, d Dispatcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	require.NoError(t, d.Shutdown(context.Background()))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}

func TestReaperStartShutdown(t *testing.T) {
	jobs := &reaperJobs{}
	d := NewDispatcher(
		&config.Config{OrchestratorDispatcherJobPollInterval: time.Millisecond},
		log.Noop(),
		jobs,
	)
	startAndCancel(t, d)
	require.GreaterOrEqual(t, jobs.reaped, 1)
}

func TestReaperReapsOnEachTick(t *testing.T) {
	jobs := &reaperJobs{}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      jobs,
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
	require.GreaterOrEqual(t, jobs.reaped, 2)
}

func TestReaperReapErrorIsLoggedAndContinues(t *testing.T) {
	jobs := &reaperJobs{reapErr: errors.New("db down")}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      jobs,
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
	// Must not have stopped — reaped multiple times despite errors.
	require.GreaterOrEqual(t, jobs.reaped, 2)
}
