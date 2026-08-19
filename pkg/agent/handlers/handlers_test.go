package handlers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/orchestrator/mock_orchestrator"
	"github.com/xyzjace/terraplane/pkg/agent/terraform/mock_terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace/mock_workspace"
	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
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

func noopWrite(_ context.Context, _ *terraplanev1.TerraformEnvelope) error { return nil }

func (s *HandlersSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.ws = mock_workspace.NewMockManager(s.ctrl)
	s.tf = mock_terraform.NewMockManager(s.ctrl)
	s.oc = mock_orchestrator.NewMockClient(s.ctrl)
}

func (s *HandlersSuite) newHandlers(write WriteFunc) *Handlers {
	return New(log.Noop(), &config.Config{AgentID: "agent-test"}, write, s.ws, s.tf, s.oc)
}

func planCmd() *terraplanev1.PlanCommand {
	return &terraplanev1.PlanCommand{
		Repo:       "acme/infra",
		PrNumber:   42,
		CommitHash: "abc123",
		StackName:  "stg",
		Dir:        "stacks/stg",
		PlanFlags:  "-target=module.vpc",
	}
}

func applyCmd() *terraplanev1.ApplyCommand {
	return &terraplanev1.ApplyCommand{
		Repo:       "acme/infra",
		PrNumber:   42,
		CommitHash: "abc123",
		StackName:  "stg",
		Dir:        "stacks/stg",
	}
}

func unlockCmd() *terraplanev1.UnlockCommand {
	return &terraplanev1.UnlockCommand{
		Repo:     "acme/infra",
		PrNumber: 42,
	}
}

// --- Plan ---

func (s *HandlersSuite) TestHandlePlanSuccessKeepsWorkspace() {
	// Intention: successful plans leave the workspace in place for a later apply.
	h := s.newHandlers(noopWrite)

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", "-target=module.vpc").Return("Plan: 1 to add", nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", true, "Plan: 1 to add", "").Return(nil)
	// No RemoveWorkspace on success.

	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanProvisionFailureWritesErrorAndSkipsTerraform() {
	h := s.newHandlers(noopWrite)

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("", errors.New("clone failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "", gomock.Any()).Return(nil)

	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanProvisionFailureSubmitErrorIsLoggedNotReturned() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("clone failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	// handlePlan has no return; must not panic and must not call terraform.
	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureRemovesWorkspace() {
	h := s.newHandlers(noopWrite)

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", "-target=module.vpc").Return("partial", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "partial", gomock.Any()).Return(nil)

	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureRemoveWorkspaceErrorStillSubmitsResult() {
	h := s.newHandlers(noopWrite)

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(errors.New("rm failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(nil)

	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureSubmitError() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanSuccessSubmitFailureRemovesWorkspace() {
	// Intention: failing to deliver a successful result still cleans up (err is set from writeErr).
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").Return(errors.New("submit failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handlePlan(context.Background(), "job-1", planCmd())
}

// --- Apply ---

func (s *HandlersSuite) TestHandleApplySuccessAlwaysRemovesWorkspace() {
	// Intention: apply always tears down the workspace, success or failure.
	h := s.newHandlers(noopWrite)

	s.ws.EXPECT().FetchWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("Apply complete!", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", true, "Apply complete!", "").Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplyFetchFailureSubmitsError() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("missing ws"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "", gomock.Any()).Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplyFetchFailureSubmitErrorIsLoggedNotReturned() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("missing ws"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplyTerraformFailureRemovesWorkspace() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("out", errors.New("apply failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", false, "out", gomock.Any()).Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplyTerraformFailureSubmitError() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("apply failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).Return(errors.New("submit failed"))

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplyRemoveWorkspaceErrorIsBestEffort() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(errors.New("rm failed"))
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplySuccessSubmitFailureStillRemovesWorkspace() {
	h := s.newHandlers(noopWrite)
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").Return(errors.New("submit failed"))

	h.handleApply(context.Background(), "job-1", applyCmd())
}

// --- Unlock ---

func (s *HandlersSuite) TestHandleUnlockSubmitsStubSuccess() {
	h := s.newHandlers(noopWrite)
	s.oc.EXPECT().SubmitResult(gomock.Any(), "job-1", "agent-test", true, gomock.Any(), "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, output, _ string) error {
			require.Contains(s.T(), output, "stub unlock")
			require.Contains(s.T(), output, "acme/infra")
			return nil
		})

	h.handleUnlock(context.Background(), "job-1", unlockCmd())
}

func (s *HandlersSuite) TestHandleUnlockSubmitFailureIsBestEffort() {
	h := s.newHandlers(noopWrite)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, gomock.Any(), "").Return(errors.New("submit failed"))

	h.handleUnlock(context.Background(), "job-1", unlockCmd())
}

// --- Dispatch / accept ---

func (s *HandlersSuite) TestDispatchUnsupportedPayloadReturnsNilWithoutAck() {
	writes := 0
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		writes++
		return nil
	})

	err := h.Dispatch(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Ack{
			Ack: &terraplanev1.Ack{Message: "noop"},
		},
	})
	require.NoError(s.T(), err)
	require.Equal(s.T(), 0, writes)
}

func (s *HandlersSuite) TestDispatchAckFailureDoesNotStartWork() {
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("cannot ack")
	})
	// No workspace/terraform expectations — work must not start.

	err := h.Dispatch(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Plan{
			Plan: planCmd(),
		},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "cannot ack")
}

func (s *HandlersSuite) TestDispatchPlanAcksThenRunsHandler() {
	done := make(chan struct{})
	var mu sync.Mutex
	var acked bool
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		mu.Lock()
		defer mu.Unlock()
		if env.GetAck() != nil {
			acked = true
			require.Equal(s.T(), "plan accepted", env.GetAck().GetMessage())
		}
		return nil
	})

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", gomock.Any()).Return("ok", nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, _, _ string) error {
			close(done)
			return nil
		})

	err := h.Dispatch(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Plan{Plan: planCmd()},
	})
	require.NoError(s.T(), err)

	select {
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		require.True(s.T(), acked)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for plan handler")
	}
}

func (s *HandlersSuite) TestDispatchApplyAcksThenRunsHandler() {
	done := make(chan struct{})
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		return nil
	})

	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, "ok", "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, _, _ string) error {
			close(done)
			return nil
		})

	err := h.Dispatch(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Apply{Apply: applyCmd()},
	})
	require.NoError(s.T(), err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for apply handler")
	}
}

func (s *HandlersSuite) TestDispatchUnlockAcksThenRunsHandler() {
	done := make(chan struct{})
	h := s.newHandlers(noopWrite)
	s.oc.EXPECT().SubmitResult(gomock.Any(), gomock.Any(), gomock.Any(), true, gomock.Any(), "").DoAndReturn(
		func(_ context.Context, _, _ string, _ bool, _, _ string) error {
			close(done)
			return nil
		})

	err := h.Dispatch(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Unlock{Unlock: unlockCmd()},
	})
	require.NoError(s.T(), err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for unlock handler")
	}
}
