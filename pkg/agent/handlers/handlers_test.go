package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/orchestrator/mock_orchestrator"
	"github.com/xyzjace/terraplane/pkg/agent/terraform/mock_terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace/mock_workspace"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
)

type HandlersSuite struct {
	suite.Suite
	ctrl *gomock.Controller
	ws   *mock_workspace.MockManager
	tf   *mock_terraform.MockManager
	oc   *mock_orchestrator.MockClient
}

func TestHandlersSuite(t *testing.T) {
	suite.Run(t, new(HandlersSuite))
}

func (s *HandlersSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.ws = mock_workspace.NewMockManager(s.ctrl)
	s.tf = mock_terraform.NewMockManager(s.ctrl)
	s.oc = mock_orchestrator.NewMockClient(s.ctrl)
}

func (s *HandlersSuite) newHandlers() *Handlers {
	return New(log.Noop(), &config.Config{AgentID: "agent-test"}, s.ws, s.tf, s.oc)
}

func planCmd() *command.PlanCommand {
	cmd := &command.PlanCommand{
		Stacks:    []string{"stg"},
		PlanFlags: "-target=module.vpc",
	}
	cmd.JobID = "job-1"
	cmd.Repo = "acme/infra"
	cmd.CommitSHA = "abc123"
	cmd.PRNumber = 42
	return cmd
}

func applyCmd() *command.ApplyCommand {
	cmd := &command.ApplyCommand{
		Stacks: []string{"stg"},
	}
	cmd.JobID = "job-1"
	cmd.Repo = "acme/infra"
	cmd.CommitSHA = "abc123"
	cmd.PRNumber = 42
	return cmd
}

func unlockCmd() *command.UnlockCommand {
	cmd := &command.UnlockCommand{}
	cmd.JobID = "job-1"
	cmd.Repo = "acme/infra"
	cmd.PRNumber = 42
	return cmd
}

// --- Plan ---

func (s *HandlersSuite) TestHandlePlanSuccessKeepsWorkspace() {
	// Intention: successful plans leave the workspace in place for a later apply.
	h := s.newHandlers()

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", "-target=module.vpc").Return("Plan: 1 to add", nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", true, "Plan: 1 to add", "").Return(nil)
	// No RemoveWorkspace on success.

	h.handlePlan(context.Background(), planCmd())
}

func (s *HandlersSuite) TestHandlePlanProvisionFailureSubmitsError() {
	h := s.newHandlers()

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("", errors.New("clone failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "", gomock.Any()).Return(nil)

	h.handlePlan(context.Background(), planCmd())
}

func (s *HandlersSuite) TestHandlePlanProvisionFailureSubmitErrorIsLoggedNotReturned() {
	h := s.newHandlers()
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("clone failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	h.handlePlan(context.Background(), planCmd())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureRemovesWorkspace() {
	h := s.newHandlers()

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", "-target=module.vpc").Return("partial", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "partial", gomock.Any()).Return(nil)

	h.handlePlan(context.Background(), planCmd())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureRemoveWorkspaceErrorStillSubmitsResult() {
	h := s.newHandlers()

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(errors.New("rm failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(nil)

	h.handlePlan(context.Background(), planCmd())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureSubmitError() {
	h := s.newHandlers()
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	h.handlePlan(context.Background(), planCmd())
}

func (s *HandlersSuite) TestHandlePlanSuccessSubmitFailureRemovesWorkspace() {
	// Intention: failing to deliver a successful result still cleans up (err is set from writeErr).
	h := s.newHandlers()
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").Return(errors.New("submit failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handlePlan(context.Background(), planCmd())
}

// --- Apply ---

func (s *HandlersSuite) TestHandleApplySuccessAlwaysRemovesWorkspace() {
	// Intention: apply always tears down the workspace, success or failure.
	h := s.newHandlers()

	s.ws.EXPECT().FetchWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("Apply complete!", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", true, "Apply complete!", "").Return(nil)

	h.handleApply(context.Background(), applyCmd())
}

func (s *HandlersSuite) TestHandleApplyFetchFailureSubmitsError() {
	h := s.newHandlers()
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("missing ws"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "", gomock.Any()).Return(nil)

	h.handleApply(context.Background(), applyCmd())
}

func (s *HandlersSuite) TestHandleApplyFetchFailureSubmitErrorIsLoggedNotReturned() {
	h := s.newHandlers()
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("missing ws"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	h.handleApply(context.Background(), applyCmd())
}

func (s *HandlersSuite) TestHandleApplyTerraformFailureRemovesWorkspace() {
	h := s.newHandlers()
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("out", errors.New("apply failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "out", gomock.Any()).Return(nil)

	h.handleApply(context.Background(), applyCmd())
}

func (s *HandlersSuite) TestHandleApplyTerraformFailureSubmitError() {
	h := s.newHandlers()
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("apply failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	h.handleApply(context.Background(), applyCmd())
}

func (s *HandlersSuite) TestHandleApplyRemoveWorkspaceErrorIsBestEffort() {
	h := s.newHandlers()
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(errors.New("rm failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").Return(nil)

	h.handleApply(context.Background(), applyCmd())
}

func (s *HandlersSuite) TestHandleApplySuccessSubmitFailureStillRemovesWorkspace() {
	h := s.newHandlers()
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").Return(errors.New("submit failed"))

	h.handleApply(context.Background(), applyCmd())
}

// --- Unlock ---

func (s *HandlersSuite) TestHandleUnlockSubmitsStubSuccess() {
	h := s.newHandlers()
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", true, gomock.Any(), "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, output, _ string) error {
			require.Contains(s.T(), output, "stub unlock")
			require.Contains(s.T(), output, "acme/infra")
			return nil
		})

	h.handleUnlock(context.Background(), unlockCmd())
}

func (s *HandlersSuite) TestHandleUnlockSubmitFailureIsBestEffort() {
	h := s.newHandlers()
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, gomock.Any(), "").Return(errors.New("submit failed"))

	h.handleUnlock(context.Background(), unlockCmd())
}

// --- Dispatch ---

func (s *HandlersSuite) TestDispatchUnknownKindIsIgnored() {
	h := s.newHandlers()
	// No mock expectations — nothing should be called.
	h.Dispatch(context.Background(), &command.Command{Kind: command.KindUnknown})
	// Give the goroutine a moment to run and confirm no panics.
	time.Sleep(50 * time.Millisecond)
}

func (s *HandlersSuite) TestDispatchPlanRunsHandler() {
	done := make(chan struct{})
	h := s.newHandlers()

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", gomock.Any()).Return("ok", nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, _, _ string) error {
			close(done)
			return nil
		})

	h.Dispatch(context.Background(), &command.Command{Kind: command.KindPlan, Plan: *planCmd()})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for plan handler")
	}
}

func (s *HandlersSuite) TestDispatchApplyRunsHandler() {
	done := make(chan struct{})
	h := s.newHandlers()

	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, _, _ string) error {
			close(done)
			return nil
		})

	h.Dispatch(context.Background(), &command.Command{Kind: command.KindApply, Apply: *applyCmd()})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for apply handler")
	}
}

func (s *HandlersSuite) TestDispatchUnlockRunsHandler() {
	done := make(chan struct{})
	h := s.newHandlers()
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, gomock.Any(), "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, _, _ string) error {
			close(done)
			return nil
		})

	h.Dispatch(context.Background(), &command.Command{Kind: command.KindUnlock, Unlock: *unlockCmd()})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for unlock handler")
	}
}
