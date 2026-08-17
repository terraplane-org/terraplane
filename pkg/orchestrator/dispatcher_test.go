package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/agentsession/mock_agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type dispatchJobs struct {
	cmds []command.Command
	err  error
}

func (s *dispatchJobs) CreatePendingJobs(context.Context, *scm.Webhook) error { return nil }
func (s *dispatchJobs) ClaimPendingJobs(context.Context, []string) ([]command.Command, error) {
	return s.cmds, s.err
}

type dispatchPlan struct {
	called int
	err    error
}

func (s *dispatchPlan) RunPlan(context.Context, command.PlanCommand) error {
	s.called++
	return s.err
}

type dispatchApply struct {
	called int
	err    error
}

func (s *dispatchApply) RunApply(context.Context, command.ApplyCommand) error {
	s.called++
	return s.err
}

type dispatchUnlock struct {
	called int
	err    error
}

func (s *dispatchUnlock) RunUnlock(context.Context, command.UnlockCommand) error {
	s.called++
	return s.err
}

type dispatchSession struct{ id string }

func (s dispatchSession) ID() string { return s.id }
func (s dispatchSession) Run(context.Context) error {
	return nil
}
func (s dispatchSession) Write(context.Context, *terraplanev1.TerraformEnvelope) error {
	return nil
}

func TestNewDispatcherStartShutdown(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().GetAllAgents().Return(nil).AnyTimes()

	d := NewDispatcher(
		&config.Config{OrchestratorDispatcherJobPollInterval: time.Millisecond},
		log.Noop(),
		&dispatchJobs{},
		nil,
		reg,
		&dispatchPlan{},
		&dispatchApply{},
		&dispatchUnlock{},
	)

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

func TestPollClaimErrorIsLogged(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().GetAllAgents().Return([]string{"agent-a"}).AnyTimes()

	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{err: errors.New("db down")},
		sessionRegistry: reg,
		jobPollInterval: time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}

func TestDispatchJobPlanApplyUnlock(t *testing.T) {
	session := dispatchSession{id: "agent-a"}
	plan := &dispatchPlan{}
	apply := &dispatchApply{}
	unlock := &dispatchUnlock{}
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), session))

	d := &dispatcher{
		logger:          log.Noop(),
		sessionRegistry: reg,
		planService:     plan,
		applyService:    apply,
		unlockService:   unlock,
	}

	planCmd := command.Command{Kind: command.KindPlan}
	planCmd.Plan.Agent = "agent-a"
	require.NoError(t, d.dispatchJob(context.Background(), planCmd))

	applyCmd := command.Command{Kind: command.KindApply}
	applyCmd.Apply.Agent = "agent-a"
	require.NoError(t, d.dispatchJob(context.Background(), applyCmd))

	unlockCmd := command.Command{Kind: command.KindUnlock}
	unlockCmd.Unlock.Agent = "agent-a"
	require.NoError(t, d.dispatchJob(context.Background(), unlockCmd))

	require.Equal(t, 1, plan.called)
	require.Equal(t, 1, apply.called)
	require.Equal(t, 1, unlock.called)
}

func TestDispatchJobUnknownKind(t *testing.T) {
	d := &dispatcher{logger: log.Noop()}
	err := d.dispatchJob(context.Background(), command.Command{Kind: command.Kind("weird")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command kind")
}

func TestDispatchJobSessionLookupError(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, errors.New("registry down"))

	d := &dispatcher{logger: log.Noop(), sessionRegistry: reg}
	cmd := command.Command{Kind: command.KindPlan}
	cmd.Plan.Agent = "agent-a"

	err := d.dispatchJob(context.Background(), cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "registry down")
}

func TestDispatchJobAgentNotConnected(t *testing.T) {
	d := &dispatcher{logger: log.Noop(), sessionRegistry: agentsession.NewRegistry(log.Noop())}
	cmd := command.Command{Kind: command.KindPlan}
	cmd.Plan.Agent = "agent-a"

	err := d.dispatchJob(context.Background(), cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not connected")
}

func TestDispatchJobRunError(t *testing.T) {
	session := dispatchSession{id: "agent-a"}
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), session))

	d := &dispatcher{
		logger:          log.Noop(),
		sessionRegistry: reg,
		planService:     &dispatchPlan{err: errors.New("plan failed")},
	}
	cmd := command.Command{Kind: command.KindPlan}
	cmd.Plan.Agent = "agent-a"

	err := d.dispatchJob(context.Background(), cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "plan failed")
}

func TestPollDispatchesClaimedJobs(t *testing.T) {
	session := dispatchSession{id: "agent-a"}
	plan := &dispatchPlan{}
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), session))

	cmd := command.Command{Kind: command.KindPlan}
	cmd.Plan.Agent = "agent-a"

	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{cmds: []command.Command{cmd}},
		sessionRegistry: reg,
		planService:     plan,
		jobPollInterval: time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
	require.GreaterOrEqual(t, plan.called, 1)
}

func TestPollDispatchErrorIsLogged(t *testing.T) {
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{cmds: []command.Command{{Kind: command.Kind("weird")}}},
		sessionRegistry: agentsession.NewRegistry(log.Noop()),
		jobPollInterval: time.Millisecond,
	}
	// Registry has no agents so poll skips claim... need agents connected.
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().GetAllAgents().Return([]string{"agent-a"}).AnyTimes()
	d.sessionRegistry = reg

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}
