package webserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
)

type recordingPlan struct {
	err    error
	called chan command.PlanCommand
}

func (s *recordingPlan) RunPlan(_ context.Context, plan command.PlanCommand) error {
	if s.called != nil {
		s.called <- plan
	}
	return s.err
}

type recordingApply struct {
	err    error
	called chan command.ApplyCommand
}

func (s *recordingApply) RunApply(_ context.Context, apply command.ApplyCommand) error {
	if s.called != nil {
		s.called <- apply
	}
	return s.err
}

type recordingUnlock struct {
	err    error
	called chan command.UnlockCommand
}

func (s *recordingUnlock) RunUnlock(_ context.Context, unlock command.UnlockCommand) error {
	if s.called != nil {
		s.called <- unlock
	}
	return s.err
}

func TestHandleCommandUnhandledKind(t *testing.T) {
	// Intention: unknown kinds after parse are warned and ignored (no panic / no service call).
	h := &handler{logger: log.Noop()}
	h.handleCommand(context.Background(), command.Command{Kind: command.Kind("weird")})
}

func TestHandleCommandDispatchesPlanApplyUnlock(t *testing.T) {
	plan := &recordingPlan{called: make(chan command.PlanCommand, 1)}
	apply := &recordingApply{called: make(chan command.ApplyCommand, 1)}
	unlock := &recordingUnlock{called: make(chan command.UnlockCommand, 1)}
	h := &handler{logger: log.Noop(), planService: plan, applyService: apply, unlockService: unlock}

	h.handleCommand(context.Background(), command.Command{
		Kind: command.KindPlan,
		Plan: command.PlanCommand{Stacks: []string{"a"}},
	})
	h.handleCommand(context.Background(), command.Command{
		Kind:  command.KindApply,
		Apply: command.ApplyCommand{Stacks: []string{"a"}},
	})
	h.handleCommand(context.Background(), command.Command{
		Kind:   command.KindUnlock,
		Unlock: command.UnlockCommand{Stacks: []string{"a"}},
	})

	select {
	case got := <-plan.called:
		require.Equal(t, []string{"a"}, got.Stacks)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for plan")
	}
	select {
	case got := <-apply.called:
		require.Equal(t, []string{"a"}, got.Stacks)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for apply")
	}
	select {
	case got := <-unlock.called:
		require.Equal(t, []string{"a"}, got.Stacks)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for unlock")
	}
}

func TestHandleCommandServiceErrorsAreLogged(t *testing.T) {
	plan := &recordingPlan{err: errors.New("plan failed"), called: make(chan command.PlanCommand, 1)}
	apply := &recordingApply{err: errors.New("apply failed"), called: make(chan command.ApplyCommand, 1)}
	unlock := &recordingUnlock{err: errors.New("unlock failed"), called: make(chan command.UnlockCommand, 1)}
	h := &handler{logger: log.Noop(), planService: plan, applyService: apply, unlockService: unlock}

	h.handleCommand(context.Background(), command.Command{Kind: command.KindPlan})
	h.handleCommand(context.Background(), command.Command{Kind: command.KindApply})
	h.handleCommand(context.Background(), command.Command{Kind: command.KindUnlock})

	<-plan.called
	<-apply.called
	<-unlock.called
}

func TestServerStartReturnsNilOnErrServerClosed(t *testing.T) {
	cfg := &config.Config{
		OrchestratorListenAddress: "127.0.0.1",
		OrchestratorListenPort:    0,
	}
	srv := NewServer(cfg, log.Noop(), nil)

	done := make(chan error, 1)
	go func() { done <- srv.Start(context.Background()) }()

	time.Sleep(20 * time.Millisecond)
	require.NoError(t, srv.Shutdown(context.Background()))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start after Shutdown")
	}
}
