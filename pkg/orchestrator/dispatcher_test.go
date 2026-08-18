package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentapi"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type dispatchJobs struct {
	cmds    []command.Command
	err     error
	failed  []string
	reapErr error
	reaped  int
}

func (s *dispatchJobs) CreatePendingJobs(context.Context, *scm.Webhook) error { return nil }
func (s *dispatchJobs) ClaimPendingJobs(context.Context, []string) ([]command.Command, error) {
	return s.cmds, s.err
}
func (s *dispatchJobs) PollJob(context.Context, string) (*agentapi.Job, error) { return nil, nil }
func (s *dispatchJobs) AckJob(context.Context, string, string) error           { return nil }
func (s *dispatchJobs) Heartbeat(context.Context, string, string) error        { return nil }
func (s *dispatchJobs) RecordResult(context.Context, string, string, agentapi.Result) error {
	return nil
}
func (s *dispatchJobs) FailClaimedJob(_ context.Context, jobID, _ string) error {
	s.failed = append(s.failed, jobID)
	return nil
}
func (s *dispatchJobs) ReapExpiredClaims(context.Context) error {
	s.reaped++
	return s.reapErr
}

type failingDispatchJobs struct {
	dispatchJobs
	failErr error
}

func (s *failingDispatchJobs) FailClaimedJob(context.Context, string, string) error {
	return s.failErr
}

type dispatchUnlock struct {
	called int
	err    error
}

func (s *dispatchUnlock) RunUnlock(context.Context, command.UnlockCommand) error {
	s.called++
	return s.err
}

func unlockDispatchCmd() command.Command {
	unlock := command.UnlockCommand{Stacks: []string{"a"}}
	unlock.JobID = "job-3"
	return command.Command{Kind: command.KindUnlock, Unlock: unlock}
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

func TestNewDispatcherStartShutdown(t *testing.T) {
	jobs := &dispatchJobs{}
	d := NewDispatcher(
		&config.Config{OrchestratorDispatcherJobPollInterval: time.Millisecond},
		log.Noop(),
		jobs,
		&dispatchUnlock{},
	)
	startAndCancel(t, d)
	require.GreaterOrEqual(t, jobs.reaped, 1)
}

func TestPollClaimErrorIsLogged(t *testing.T) {
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{err: errors.New("db down")},
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
}

func TestPollReapErrorStillClaims(t *testing.T) {
	jobs := &dispatchJobs{reapErr: errors.New("reap failed")}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      jobs,
		jobPollInterval: time.Millisecond,
		unlockService:   &dispatchUnlock{},
	}
	startAndCancel(t, d)
	require.GreaterOrEqual(t, jobs.reaped, 1)
}

func TestDispatchUnlock(t *testing.T) {
	unlock := &dispatchUnlock{}
	d := &dispatcher{logger: log.Noop(), jobService: &dispatchJobs{}, unlockService: unlock}
	require.NoError(t, d.dispatchUnlock(context.Background(), unlockDispatchCmd()))
	require.Equal(t, 1, unlock.called)
}

func TestDispatchUnlockErrorMarksFailed(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{
		logger:        log.Noop(),
		jobService:    jobs,
		unlockService: &dispatchUnlock{err: errors.New("unlock failed")},
	}
	err := d.dispatchUnlock(context.Background(), unlockDispatchCmd())
	require.Error(t, err)
	require.Equal(t, []string{"job-3"}, jobs.failed)
}

func TestDispatchNonUnlockMarksFailed(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{logger: log.Noop(), jobService: jobs}
	cmd := command.Command{Kind: command.KindPlan}
	cmd.Plan.JobID = "job-1"
	require.NoError(t, d.dispatchUnlock(context.Background(), cmd))
	require.Equal(t, []string{"job-1"}, jobs.failed)
}

func TestFailClaimLogsRepositoryError(t *testing.T) {
	d := &dispatcher{logger: log.Noop(), jobService: &failingDispatchJobs{failErr: errors.New("fail failed")}}
	d.failClaim(context.Background(), "job-1", "nope")
	d.failClaim(context.Background(), "", "nope")
}

func TestPollDispatchesUnlock(t *testing.T) {
	unlock := &dispatchUnlock{}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{cmds: []command.Command{unlockDispatchCmd()}},
		unlockService:   unlock,
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
	require.GreaterOrEqual(t, unlock.called, 1)
}

func TestPollDispatchErrorIsLogged(t *testing.T) {
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{cmds: []command.Command{unlockDispatchCmd()}},
		unlockService:   &dispatchUnlock{err: errors.New("nope")},
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
}

func TestCommandJobID(t *testing.T) {
	plan := command.Command{Kind: command.KindPlan}
	plan.Plan.JobID = "p"
	apply := command.Command{Kind: command.KindApply}
	apply.Apply.JobID = "a"
	require.Equal(t, "p", commandJobID(plan))
	require.Equal(t, "a", commandJobID(apply))
	require.Equal(t, "job-3", commandJobID(unlockDispatchCmd()))
	require.Equal(t, "", commandJobID(command.Command{}))
}
