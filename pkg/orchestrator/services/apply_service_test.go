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

type ApplyServiceSuite struct {
	suite.Suite
	ctrl     *gomock.Controller
	registry *mock_agentsession.MockRegistry
	scm      *mock_scm.MockProvider
	svc      services.ApplyService
}

func TestApplyServiceSuite(t *testing.T) {
	suite.Run(t, new(ApplyServiceSuite))
}

func (s *ApplyServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.registry = mock_agentsession.NewMockRegistry(s.ctrl)
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.svc = services.NewApplyService(log.Noop(), s.registry, s.scm)
}

func (s *ApplyServiceSuite) TestFetchConfigFailure() {
	apply := applyCmd("terraplane apply")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return("", errors.New("404"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch terraplane.yaml")
}

func (s *ApplyServiceSuite) TestParseConfigFailure() {
	apply := applyCmd("terraplane apply")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return("stacks: [", nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to parse terraplane.yaml")
}

func (s *ApplyServiceSuite) TestResolveStacksFailure() {
	apply := applyCmd("terraplane apply -s missing")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to resolve stacks")
}

func (s *ApplyServiceSuite) TestNoAgentsConnectedReturnsNilWithoutDispatch() {
	apply := applyCmd("terraplane apply")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(nil, nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestPreflightRegistryError() {
	apply := applyCmd("terraplane apply")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, errors.New("registry down"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to look up agent sessions")
}

func (s *ApplyServiceSuite) TestDispatchFailure() {
	apply := applyCmd("terraplane apply -s a")
	apply.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	session.EXPECT().Write(gomock.Any(), gomock.Any()).Return(errors.New("agent gone"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to dispatch apply")
}

func (s *ApplyServiceSuite) TestSkipsDisconnectedAgentContinuesWithConnected() {
	apply := applyCmd("terraplane apply")
	apply.JobID = "job-1"
	sessionB := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(sessionB, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(sessionB, nil)

	sessionB.EXPECT().Write(gomock.Any(), gomock.AssignableToTypeOf(&terraplanev1.TerraformEnvelope{})).DoAndReturn(
		func(_ context.Context, env *terraplanev1.TerraformEnvelope) error {
			require.Equal(s.T(), "job-1", env.GetJobId())
			require.Equal(s.T(), "b", env.GetApply().StackName)
			return nil
		},
	)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestLoopRegistryErrorAborts() {
	apply := applyCmd("terraplane apply")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, errors.New("lookup failed"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to look up agent session")
}

func (s *ApplyServiceSuite) TestHappyPath() {
	apply := applyCmd("terraplane apply -s a")
	apply.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	session.EXPECT().Write(gomock.Any(), gomock.Any()).Return(nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestPreflightHitThenLoopMissesAll() {
	apply := applyCmd("terraplane apply -s a")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}
