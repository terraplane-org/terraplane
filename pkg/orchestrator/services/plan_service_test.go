package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/pkg/agentsession/mock_agentsession"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type PlanServiceSuite struct {
	suite.Suite
	ctrl     *gomock.Controller
	registry *mock_agentsession.MockRegistry
	scm      *mock_scm.MockProvider
	svc      services.PlanService
}

func TestPlanServiceSuite(t *testing.T) {
	suite.Run(t, new(PlanServiceSuite))
}

func (s *PlanServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.registry = mock_agentsession.NewMockRegistry(s.ctrl)
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.svc = services.NewPlanService(log.Noop(), s.registry, s.scm)
}

func (s *PlanServiceSuite) TestFetchConfigFailure() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return("", errors.New("404"))

	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch terraplane.yaml")
}

func (s *PlanServiceSuite) TestParseConfigFailure() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return("stacks: [", nil)

	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to parse terraplane.yaml")
}

func (s *PlanServiceSuite) TestResolveStacksFailure() {
	plan := planCmd("terraplane plan -s missing")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)

	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to resolve stacks")
}

func (s *PlanServiceSuite) TestNoAgentsConnectedReturnsNilWithoutDispatch() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(nil, nil)
	// No jobs.Create — preflight short-circuits before dispatch.

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}

func (s *PlanServiceSuite) TestNoAgentsConnectedDedupesSharedAgentInPreflight() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(sameAgentYAML, nil)
	// Unique agent ID is looked up once even though two stacks reference it.
	s.registry.EXPECT().Get(gomock.Any(), "shared").Return(nil, nil)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}

func (s *PlanServiceSuite) TestPreflightRegistryError() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, errors.New("registry down"))

	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to look up agent sessions")
}

func (s *PlanServiceSuite) TestDispatchesConnectedStacksAndSkipsDisconnected() {
	plan := planCmd("terraplane plan")
	plan.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	// Preflight finds agent-a connected and stops.
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil)
	// Loop lookups.
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(nil, nil)

	session.EXPECT().Write(gomock.Any(), gomock.AssignableToTypeOf(&terraplanev1.TerraformEnvelope{})).DoAndReturn(
		func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
			require.Equal(s.T(), "job-1", env.GetJobId())
			planCmd := env.GetPlan()
			require.NotNil(s.T(), planCmd)
			require.Equal(s.T(), "a", planCmd.StackName)
			require.Equal(s.T(), "stacks/a", planCmd.Dir)
			return nil
		},
	)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}

func (s *PlanServiceSuite) TestSharedAgentPreflightChecksOnce() {
	plan := planCmd("terraplane plan")
	plan.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(sameAgentYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "shared").Return(session, nil) // preflight
	s.registry.EXPECT().Get(gomock.Any(), "shared").Return(session, nil) // stack a
	s.registry.EXPECT().Get(gomock.Any(), "shared").Return(session, nil) // stack b

	session.EXPECT().Write(gomock.Any(), gomock.Any()).Return(nil).Times(2)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}

func (s *PlanServiceSuite) TestDispatchFailure() {
	plan := planCmd("terraplane plan -s a")
	plan.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	session.EXPECT().Write(gomock.Any(), gomock.Any()).Return(errors.New("agent gone"))

	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to dispatch plan")
}

func (s *PlanServiceSuite) TestLoopRegistryErrorAborts() {
	plan := planCmd("terraplane plan")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil) // preflight
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, errors.New("lookup failed"))

	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to look up agent session")
}

func (s *PlanServiceSuite) TestPlanFlagsForwardedToAgent() {
	plan := planCmd("terraplane plan -s a -- -target=module.vpc")
	plan.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	session.EXPECT().Write(gomock.Any(), gomock.AssignableToTypeOf(&terraplanev1.TerraformEnvelope{})).DoAndReturn(
		func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
			require.Equal(s.T(), "job-1", env.GetJobId())
			require.Equal(s.T(), "-target=module.vpc", env.GetPlan().PlanFlags)
			return nil
		},
	)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}

func (s *PlanServiceSuite) TestEnvironmentFlagDispatchesAllStacksInEnv() {
	plan := planCmd("terraplane plan -e staging")
	plan.JobID = "job-1"
	sessionA := mock_agentsession.NewMockSession(s.ctrl)
	sessionB := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoEnvYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(sessionA, nil).Times(2) // preflight + dispatch
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(sessionB, nil)          // dispatch only
	sessionA.EXPECT().Write(gomock.Any(), gomock.Any()).Return(nil)
	sessionB.EXPECT().Write(gomock.Any(), gomock.Any()).Return(nil)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}
