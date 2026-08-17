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
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type ApplyServiceSuite struct {
	suite.Suite
	ctrl     *gomock.Controller
	registry *mock_agentsession.MockRegistry
	scm      *mock_scm.MockProvider
	locks    *mock_repository.MockLockRepository
	svc      services.ApplyService
}

func TestApplyServiceSuite(t *testing.T) {
	suite.Run(t, new(ApplyServiceSuite))
}

func (s *ApplyServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.registry = mock_agentsession.NewMockRegistry(s.ctrl)
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.locks = mock_repository.NewMockLockRepository(s.ctrl)
	s.svc = services.NewApplyService(log.Noop(), s.registry, s.scm, s.locks)
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

func (s *ApplyServiceSuite) TestNoAgentsConnectedReturnsNilWithoutLockOrJob() {
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

func (s *ApplyServiceSuite) TestAllStacksLockedReturnsNilWithoutDispatch() {
	apply := applyCmd("terraplane apply")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil) // preflight
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(session, nil)

	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(repository.ErrLockExists).Times(2)
	s.locks.EXPECT().Get(gomock.Any(), apply.Repo, "a", "default").Return(&models.ProjectLock{
		PRNumber: 7,
		LockedBy: "other",
	}, nil)
	s.locks.EXPECT().Get(gomock.Any(), apply.Repo, "b", "default").Return(&models.ProjectLock{
		PRNumber: 7,
		LockedBy: "other",
	}, nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestLockExistsGetFailure() {
	apply := applyCmd("terraplane apply -s a")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), apply.Repo, "a", "default").Return(nil, errors.New("db"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch lock")
}

func (s *ApplyServiceSuite) TestLockCreateHardFailureAborts() {
	apply := applyCmd("terraplane apply")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil) // preflight
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(errors.New("db"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to create lock")
}

func (s *ApplyServiceSuite) TestPartialDispatchSkipsLockedContinues() {
	apply := applyCmd("terraplane apply")
	apply.JobID = "job-1"
	sessionA := mock_agentsession.NewMockSession(s.ctrl)
	sessionB := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(sessionA, nil) // preflight
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(sessionA, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(sessionB, nil)

	s.locks.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&models.ProjectLock{})).DoAndReturn(
		func(_ context.Context, lock *models.ProjectLock) error {
			if lock.StackName == "a" {
				return repository.ErrLockExists
			}
			return nil
		},
	).Times(2)
	s.locks.EXPECT().Get(gomock.Any(), apply.Repo, "a", "default").Return(&models.ProjectLock{PRNumber: 1, LockedBy: "x"}, nil)

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

func (s *ApplyServiceSuite) TestDispatchFailureReleasesLock() {
	apply := applyCmd("terraplane apply -s a")
	apply.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	session.EXPECT().Write(gomock.Any(), gomock.Any()).Return(errors.New("agent gone"))
	s.locks.EXPECT().Delete(gomock.Any(), apply.Repo, "a", "default").Return(nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to dispatch apply")
	require.NotContains(s.T(), err.Error(), "also failed")
}

func (s *ApplyServiceSuite) TestDispatchFailureLockReleaseFailure() {
	apply := applyCmd("terraplane apply -s a")
	apply.JobID = "job-1"
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	session.EXPECT().Write(gomock.Any(), gomock.Any()).Return(errors.New("agent gone"))
	s.locks.EXPECT().Delete(gomock.Any(), apply.Repo, "a", "default").Return(errors.New("unlock failed"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "also failed to release lock")
}

func (s *ApplyServiceSuite) TestSkipsDisconnectedAgentContinuesWithConnected() {
	apply := applyCmd("terraplane apply")
	apply.JobID = "job-1"
	sessionB := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, nil)      // preflight: miss
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(sessionB, nil) // preflight: hit
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, nil)      // loop
	s.registry.EXPECT().Get(gomock.Any(), "agent-b").Return(sessionB, nil)

	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	sessionB.EXPECT().Write(gomock.Any(), gomock.Any()).Return(nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestLoopRegistryErrorAborts() {
	apply := applyCmd("terraplane apply")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil) // preflight
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
	s.locks.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&models.ProjectLock{})).DoAndReturn(
		func(_ context.Context, lock *models.ProjectLock) error {
			require.Equal(s.T(), "a", lock.StackName)
			require.Equal(s.T(), "default", lock.Workspace)
			require.Equal(s.T(), apply.TriggerUser, lock.LockedBy)
			return nil
		},
	)
	session.EXPECT().Write(gomock.Any(), gomock.Any()).Return(nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestLockExistsWithNilLockStillSkips() {
	apply := applyCmd("terraplane apply -s a")
	session := mock_agentsession.NewMockSession(s.ctrl)

	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.registry.EXPECT().Get(gomock.Any(), "agent-a").Return(session, nil).Times(2)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), apply.Repo, "a", "default").Return(nil, nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}
