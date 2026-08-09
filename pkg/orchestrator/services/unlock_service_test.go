package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
)

type UnlockServiceSuite struct {
	suite.Suite
	ctrl      *gomock.Controller
	scm       *mock_scm.MockProvider
	publisher *mock_scm.MockPublisher
	jobs      *mock_repository.MockJobRepository
	locks     *mock_repository.MockLockRepository
	svc       services.UnlockService
}

func TestUnlockServiceSuite(t *testing.T) {
	suite.Run(t, new(UnlockServiceSuite))
}

func (s *UnlockServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.publisher = mock_scm.NewMockPublisher(s.ctrl)
	s.jobs = mock_repository.NewMockJobRepository(s.ctrl)
	s.locks = mock_repository.NewMockLockRepository(s.ctrl)
	s.svc = services.NewUnlockService(log.Noop(), s.scm, s.publisher, s.jobs, s.locks)
}

func (s *UnlockServiceSuite) TestFetchConfigFailurePublishesFailureComment() {
	unlock := unlockCmd("terraplane unlock")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return("", errors.New("404"))
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, _ int, body string) error {
			require.Contains(s.T(), body, "**terraplane** · unlock")
			require.Contains(s.T(), body, "failed")
			require.Contains(s.T(), body, "failed to fetch terraplane.yaml")
			return nil
		},
	)

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch terraplane.yaml")
}

func (s *UnlockServiceSuite) TestFetchConfigFailureCommentIsBestEffort() {
	unlock := unlockCmd("terraplane unlock")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return("", errors.New("404"))
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, gomock.Any()).Return(errors.New("github down"))

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch terraplane.yaml")
	require.NotContains(s.T(), err.Error(), "github down")
}

func (s *UnlockServiceSuite) TestParseConfigFailure() {
	unlock := unlockCmd("terraplane unlock")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return("stacks: [", nil)
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, gomock.Any()).Return(nil)

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to parse terraplane.yaml")
}

func (s *UnlockServiceSuite) TestResolveStacksFailure() {
	unlock := unlockCmd("terraplane unlock -s missing")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return(twoStackYAML, nil)
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, gomock.Any()).Return(nil)

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to resolve stacks")
}

func (s *UnlockServiceSuite) TestLockDeleteFailurePublishesPerStack() {
	unlock := unlockCmd("terraplane unlock")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return(twoStackYAML, nil)
	s.locks.EXPECT().DeleteByRepoAndStacks(gomock.Any(), unlock.Repo, []string{"a", "b"}).Return(0, errors.New("db"))
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, _ int, body string) error {
			require.Contains(s.T(), body, "**terraplane** · unlock")
			require.Contains(s.T(), body, "failed")
			return nil
		},
	).Times(2)

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to delete project locks")
}

func (s *UnlockServiceSuite) TestJobDeleteFailurePublishesPerStack() {
	unlock := unlockCmd("terraplane unlock -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return(twoStackYAML, nil)
	s.locks.EXPECT().DeleteByRepoAndStacks(gomock.Any(), unlock.Repo, []string{"a"}).Return(1, nil)
	s.jobs.EXPECT().DeleteByRepoPRAndStacks(gomock.Any(), unlock.Repo, unlock.PRNumber, []string{"a"}).Return(0, errors.New("db"))
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, gomock.Any()).Return(nil)

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to delete jobs")
}

func (s *UnlockServiceSuite) TestSuccessPublishesPerStackComments() {
	unlock := unlockCmd("terraplane unlock")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return(twoStackYAML, nil)
	s.locks.EXPECT().DeleteByRepoAndStacks(gomock.Any(), unlock.Repo, []string{"a", "b"}).Return(2, nil)
	s.jobs.EXPECT().DeleteByRepoPRAndStacks(gomock.Any(), unlock.Repo, unlock.PRNumber, []string{"a", "b"}).Return(2, nil)

	expectedA := feedback.UnlockResultComment("a", true, "")
	expectedB := feedback.UnlockResultComment("b", true, "")
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, expectedA).Return(nil)
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, expectedB).Return(nil)

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.NoError(s.T(), err)
}

func (s *UnlockServiceSuite) TestSuccessCommentFailureIsBestEffort() {
	unlock := unlockCmd("terraplane unlock -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo).Return(twoStackYAML, nil)
	s.locks.EXPECT().DeleteByRepoAndStacks(gomock.Any(), unlock.Repo, []string{"a"}).Return(1, nil)
	s.jobs.EXPECT().DeleteByRepoPRAndStacks(gomock.Any(), unlock.Repo, unlock.PRNumber, []string{"a"}).Return(1, nil)
	s.publisher.EXPECT().WriteComment(gomock.Any(), unlock.Repo, unlock.PRNumber, gomock.Any()).Return(errors.New("github down"))

	err := s.svc.RunUnlock(context.Background(), unlock)
	require.NoError(s.T(), err)
}
