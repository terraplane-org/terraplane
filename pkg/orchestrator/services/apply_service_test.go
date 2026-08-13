package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
)

type ApplyServiceSuite struct {
	suite.Suite
	ctrl  *gomock.Controller
	scm   *mock_scm.MockProvider
	jobs  *mock_repository.MockJobRepository
	locks *mock_repository.MockLockRepository
	svc   services.ApplyService
}

func TestApplyServiceSuite(t *testing.T) {
	suite.Run(t, new(ApplyServiceSuite))
}

func (s *ApplyServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.jobs = mock_repository.NewMockJobRepository(s.ctrl)
	s.locks = mock_repository.NewMockLockRepository(s.ctrl)
	s.svc = services.NewApplyService(log.Noop(), s.scm, s.jobs, s.locks)
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

func (s *ApplyServiceSuite) TestEnqueueCreatesLock() {
	apply := applyCmd("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(nil, nil)
	s.locks.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&models.ProjectLock{})).Return(nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, job *models.Job) (*models.Job, error) {
			require.Equal(s.T(), models.JobActionApply, job.Action)
			require.Equal(s.T(), "agent-a", job.AgentID)
			return job, nil
		},
	)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestSupersedePendingSkipsNewLock() {
	apply := applyCmd("terraplane apply -s a")
	existing := &models.Job{ID: "old", Repo: apply.Repo, StackName: "a", Action: models.JobActionApply, Status: models.JobStatusPending}
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(existing, nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.Any()).Return(existing, nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestSkipWhenLocked() {
	apply := applyCmd("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(nil, nil)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), apply.Repo, "a", "default").Return(&models.ProjectLock{
		PRNumber: 9,
		LockedBy: "other",
	}, nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.NoError(s.T(), err)
}

func (s *ApplyServiceSuite) TestEnqueueFailureReleasesLock() {
	apply := applyCmd("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(nil, nil)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.Any()).Return(nil, errors.New("db down"))
	s.locks.EXPECT().Delete(gomock.Any(), apply.Repo, "a", "default").Return(nil)

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to enqueue apply job")
}

func (s *ApplyServiceSuite) TestEnqueueFailureAndLockReleaseFailure() {
	apply := applyCmd("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(nil, nil)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.Any()).Return(nil, errors.New("db down"))
	s.locks.EXPECT().Delete(gomock.Any(), apply.Repo, "a", "default").Return(errors.New("unlock failed"))

	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "also failed to release lock")
}

func (s *ApplyServiceSuite) TestGetPendingError() {
	apply := applyCmd("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(nil, errors.New("db"))
	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to look up pending apply")
}

func (s *ApplyServiceSuite) TestLockCreateError() {
	apply := applyCmd("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(nil, nil)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to create lock")
}

func (s *ApplyServiceSuite) TestLockedGetError() {
	apply := applyCmd("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().GetPending(gomock.Any(), apply.Repo, "a", models.JobActionApply).Return(nil, nil)
	s.locks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), apply.Repo, "a", "default").Return(nil, errors.New("db"))
	err := s.svc.RunApply(context.Background(), apply)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch lock")
}
