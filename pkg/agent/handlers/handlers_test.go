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
}

func TestHandlersSuite(t *testing.T) {
	suite.Run(t, new(HandlersSuite))
}

func (s *HandlersSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.ws = mock_workspace.NewMockManager(s.ctrl)
	s.tf = mock_terraform.NewMockManager(s.ctrl)
}

func (s *HandlersSuite) newHandlers(write WriteFunc) *Handlers {
	return New(log.Noop(), write, s.ws, s.tf)
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
	var got *terraplanev1.PlanResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetPlanResult()
		return nil
	})

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", "-target=module.vpc").Return("Plan: 1 to add", nil)
	// No RemoveWorkspace on success.

	h.handlePlan(context.Background(), "job-1", planCmd())
	require.NotNil(s.T(), got)
	require.True(s.T(), got.GetSuccess())
	require.Equal(s.T(), "Plan: 1 to add", got.GetOutput())
}

func (s *HandlersSuite) TestHandlePlanProvisionFailureWritesErrorAndSkipsTerraform() {
	var got *terraplanev1.PlanResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetPlanResult()
		return nil
	})

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("", errors.New("clone failed"))

	h.handlePlan(context.Background(), "job-1", planCmd())
	require.False(s.T(), got.GetSuccess())
	require.Contains(s.T(), got.GetError(), "clone failed")
}

func (s *HandlersSuite) TestHandlePlanProvisionFailureWriteErrorIsLoggedNotReturned() {
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("websocket down")
	})
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("clone failed"))

	// handlePlan has no return; must not panic and must not call terraform.
	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureRemovesWorkspace() {
	var got *terraplanev1.PlanResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetPlanResult()
		return nil
	})

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", "-target=module.vpc").Return("partial", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handlePlan(context.Background(), "job-1", planCmd())
	require.False(s.T(), got.GetSuccess())
	require.Equal(s.T(), "partial", got.GetOutput())
	require.Contains(s.T(), got.GetError(), "tf failed")
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureRemoveWorkspaceErrorStillWritesResult() {
	var got *terraplanev1.PlanResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetPlanResult()
		return nil
	})

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(errors.New("rm failed"))

	h.handlePlan(context.Background(), "job-1", planCmd())
	require.False(s.T(), got.GetSuccess())
}

func (s *HandlersSuite) TestHandlePlanTerraformFailureWriteError() {
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("websocket down")
	})
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("tf failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handlePlan(context.Background(), "job-1", planCmd())
}

func (s *HandlersSuite) TestHandlePlanSuccessWriteFailureRemovesWorkspace() {
	// Intention: failing to deliver a successful result still cleans up (err is set from writeErr).
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("websocket down")
	})
	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handlePlan(context.Background(), "job-1", planCmd())
}

// --- Apply ---

func (s *HandlersSuite) TestHandleApplySuccessAlwaysRemovesWorkspace() {
	// Intention: apply always tears down the workspace, success or failure.
	var got *terraplanev1.ApplyResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetApplyResult()
		return nil
	})

	s.ws.EXPECT().FetchWorkspace(gomock.Any(), "acme/infra", "abc123", "stg").Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("Apply complete!", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
	require.True(s.T(), got.GetSuccess())
	require.Equal(s.T(), "Apply complete!", got.GetOutput())
}

func (s *HandlersSuite) TestHandleApplyFetchFailureWritesError() {
	var got *terraplanev1.ApplyResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetApplyResult()
		return nil
	})
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("missing ws"))

	h.handleApply(context.Background(), "job-1", applyCmd())
	require.False(s.T(), got.GetSuccess())
	require.Contains(s.T(), got.GetError(), "missing ws")
}

func (s *HandlersSuite) TestHandleApplyFetchFailureWriteError() {
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("websocket down")
	})
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("missing ws"))

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplyTerraformFailureRemovesWorkspace() {
	var got *terraplanev1.ApplyResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetApplyResult()
		return nil
	})
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("out", errors.New("apply failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
	require.False(s.T(), got.GetSuccess())
	require.Equal(s.T(), "out", got.GetOutput())
}

func (s *HandlersSuite) TestHandleApplyTerraformFailureWriteError() {
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("websocket down")
	})
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("apply failed"))
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
}

func (s *HandlersSuite) TestHandleApplyRemoveWorkspaceErrorIsBestEffort() {
	var got *terraplanev1.ApplyResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetApplyResult()
		return nil
	})
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(errors.New("rm failed"))

	h.handleApply(context.Background(), "job-1", applyCmd())
	require.True(s.T(), got.GetSuccess())
}

func (s *HandlersSuite) TestHandleApplySuccessWriteFailureStillRemovesWorkspace() {
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("websocket down")
	})
	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), gomock.Any(), gomock.Any()).Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

	h.handleApply(context.Background(), "job-1", applyCmd())
}

// --- Unlock ---

func (s *HandlersSuite) TestHandleUnlockWritesStubSuccess() {
	var got *terraplanev1.UnlockResult
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		got = env.GetUnlockResult()
		return nil
	})

	h.handleUnlock(context.Background(), "job-1", unlockCmd())
	require.True(s.T(), got.GetSuccess())
	require.Contains(s.T(), got.GetOutput(), "stub unlock")
	require.Contains(s.T(), got.GetOutput(), "acme/infra")
}

func (s *HandlersSuite) TestHandleUnlockWriteFailureIsBestEffort() {
	h := s.newHandlers(func(context.Context, *terraplanev1.TerraformEnvelope) error {
		return errors.New("websocket down")
	})
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
	done := make(chan *terraplanev1.PlanResult, 1)
	var mu sync.Mutex
	var acked bool
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		mu.Lock()
		defer mu.Unlock()
		if env.GetAck() != nil {
			acked = true
			require.Equal(s.T(), "plan accepted", env.GetAck().GetMessage())
			return nil
		}
		done <- env.GetPlanResult()
		return nil
	})

	s.ws.EXPECT().ProvisionWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunPlan(gomock.Any(), "/tmp/ws", "stg", gomock.Any()).Return("ok", nil)

	err := h.Dispatch(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Plan{Plan: planCmd()},
	})
	require.NoError(s.T(), err)

	select {
	case result := <-done:
		require.True(s.T(), acked)
		require.True(s.T(), result.GetSuccess())
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for plan handler")
	}
}

func (s *HandlersSuite) TestDispatchApplyAcksThenRunsHandler() {
	done := make(chan struct{})
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		if env.GetApplyResult() != nil {
			close(done)
		}
		return nil
	})

	s.ws.EXPECT().FetchWorkspace(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/tmp/ws", nil)
	s.tf.EXPECT().RunApply(gomock.Any(), "/tmp/ws", "stg").Return("ok", nil)
	s.ws.EXPECT().RemoveWorkspace(gomock.Any(), "/tmp/ws").Return(nil)

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
	h := s.newHandlers(func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
		if env.GetUnlockResult() != nil {
			close(done)
		}
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
