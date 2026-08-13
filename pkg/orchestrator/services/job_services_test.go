package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
)

type JobServicesSuite struct {
	suite.Suite
	ctrl      *gomock.Controller
	jobs      *mock_repository.MockJobRepository
	locks     *mock_repository.MockLockRepository
	publisher *mock_scm.MockPublisher
	claim     services.JobClaimService
	result    services.JobResultService
}

func TestJobServicesSuite(t *testing.T) {
	suite.Run(t, new(JobServicesSuite))
}

func (s *JobServicesSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.jobs = mock_repository.NewMockJobRepository(s.ctrl)
	s.locks = mock_repository.NewMockLockRepository(s.ctrl)
	s.publisher = mock_scm.NewMockPublisher(s.ctrl)
	s.claim = services.NewJobClaimService(log.Noop(), s.jobs, time.Hour)
	s.result = services.NewJobResultService(log.Noop(), s.jobs, s.locks, s.publisher)
}

func (s *JobServicesSuite) TestClaimRequiresAgentID() {
	_, err := s.claim.Claim(context.Background(), "", 0)
	require.Error(s.T(), err)
}

func (s *JobServicesSuite) TestClaimImmediateMiss() {
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", time.Hour).Return(nil, nil)
	job, err := s.claim.Claim(context.Background(), "agent-dev", 0)
	require.NoError(s.T(), err)
	require.Nil(s.T(), job)
}

func (s *JobServicesSuite) TestClaimReturnsJob() {
	want := &models.Job{ID: "j1", AgentID: "agent-dev", Action: models.JobActionPlan}
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", time.Hour).Return(want, nil)
	job, err := s.claim.Claim(context.Background(), "agent-dev", 0)
	require.NoError(s.T(), err)
	require.Equal(s.T(), want, job)
}

func (s *JobServicesSuite) TestClaimError() {
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", time.Hour).Return(nil, errors.New("db"))
	_, err := s.claim.Claim(context.Background(), "agent-dev", 0)
	require.Error(s.T(), err)
}

func (s *JobServicesSuite) TestClaimFromConfig() {
	svc := services.NewJobClaimServiceFromConfig(log.Noop(), s.jobs, &config.Config{OrchestratorJobLeaseDuration: 2 * time.Hour})
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", 2*time.Hour).Return(nil, nil)
	_, err := svc.Claim(context.Background(), "agent-dev", 0)
	require.NoError(s.T(), err)
}

func (s *JobServicesSuite) TestCompletePlanSuccess() {
	job := &models.Job{
		ID: "j1", AgentID: "agent-dev", Action: models.JobActionPlan,
		Repo: "acme/infra", PRNumber: 1, StackName: "a", Status: models.JobStatusRunning,
	}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusSucceeded, updated.Status)
			require.Equal(s.T(), "out", updated.Output)
			return nil
		},
	)
	s.publisher.EXPECT().WriteComment(gomock.Any(), "acme/infra", 1, gomock.Any()).Return(nil)
	require.NoError(s.T(), s.result.Complete(context.Background(), "agent-dev", "j1", true, "out", ""))
}

func (s *JobServicesSuite) TestCompleteApplyReleasesLock() {
	job := &models.Job{
		ID: "j1", AgentID: "agent-dev", Action: models.JobActionApply,
		Repo: "acme/infra", PRNumber: 1, StackName: "a", Status: models.JobStatusRunning,
	}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.locks.EXPECT().Delete(gomock.Any(), "acme/infra", "a", "default").Return(nil)
	s.publisher.EXPECT().WriteComment(gomock.Any(), "acme/infra", 1, gomock.Any()).Return(nil)
	require.NoError(s.T(), s.result.Complete(context.Background(), "agent-dev", "j1", false, "", "boom"))
}

func (s *JobServicesSuite) TestCompleteWrongAgent() {
	job := &models.Job{ID: "j1", AgentID: "other", Status: models.JobStatusRunning}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	err := s.result.Complete(context.Background(), "agent-dev", "j1", true, "", "")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "not assigned")
}

func (s *JobServicesSuite) TestCompleteInactiveJob() {
	job := &models.Job{ID: "j1", AgentID: "agent-dev", Status: models.JobStatusSucceeded}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	err := s.result.Complete(context.Background(), "agent-dev", "j1", true, "", "")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "not active")
}

func (s *JobServicesSuite) TestFailExpired() {
	expired := []models.Job{{
		ID: "j1", Action: models.JobActionApply, Repo: "acme/infra", PRNumber: 1, StackName: "a",
		ErrorMsg: "job lease expired before the agent reported a result",
	}}
	s.jobs.EXPECT().FailExpiredLeases(gomock.Any(), gomock.Any()).Return(expired, nil)
	s.locks.EXPECT().Delete(gomock.Any(), "acme/infra", "a", "default").Return(nil)
	s.publisher.EXPECT().WriteComment(gomock.Any(), "acme/infra", 1, gomock.Any()).Return(nil)
	require.NoError(s.T(), s.result.FailExpired(context.Background()))
}

func (s *JobServicesSuite) TestClaimDefaultLeaseAndWaitLoop() {
	svc := services.NewJobClaimService(log.Noop(), s.jobs, 0)
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", time.Hour).Return(nil, nil)
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", time.Hour).Return(&models.Job{ID: "j1"}, nil)
	job, err := svc.Claim(context.Background(), "agent-dev", 2*time.Second)
	require.NoError(s.T(), err)
	require.Equal(s.T(), "j1", job.ID)
}

func (s *JobServicesSuite) TestClaimContextCanceled() {
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", time.Hour).Return(nil, nil).AnyTimes()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.claim.Claim(ctx, "agent-dev", time.Second)
	require.Error(s.T(), err)
}

func (s *JobServicesSuite) TestCompleteGetFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(nil, errors.New("missing"))
	err := s.result.Complete(context.Background(), "agent-dev", "j1", true, "", "")
	require.Error(s.T(), err)
}

func (s *JobServicesSuite) TestCompleteUpdateFailureReleasesApplyLock() {
	job := &models.Job{
		ID: "j1", AgentID: "agent-dev", Action: models.JobActionApply,
		Repo: "acme/infra", StackName: "a", Status: models.JobStatusRunning,
	}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	s.locks.EXPECT().Delete(gomock.Any(), "acme/infra", "a", "default").Return(nil)
	err := s.result.Complete(context.Background(), "agent-dev", "j1", true, "", "")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to update job")
}

func (s *JobServicesSuite) TestCompleteUpdateAndLockReleaseFailure() {
	job := &models.Job{
		ID: "j1", AgentID: "agent-dev", Action: models.JobActionApply,
		Repo: "acme/infra", StackName: "a", Status: models.JobStatusRunning,
	}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	s.locks.EXPECT().Delete(gomock.Any(), "acme/infra", "a", "default").Return(errors.New("lock"))
	err := s.result.Complete(context.Background(), "agent-dev", "j1", true, "", "")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "also failed to release lock")
}

func (s *JobServicesSuite) TestCompleteApplyLockReleaseFailureAfterUpdate() {
	job := &models.Job{
		ID: "j1", AgentID: "agent-dev", Action: models.JobActionApply,
		Repo: "acme/infra", PRNumber: 1, StackName: "a", Status: models.JobStatusRunning,
	}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.locks.EXPECT().Delete(gomock.Any(), "acme/infra", "a", "default").Return(errors.New("lock"))
	err := s.result.Complete(context.Background(), "agent-dev", "j1", true, "out", "")
	require.Error(s.T(), err)
}

func (s *JobServicesSuite) TestCompleteCommentFailureStillSucceeds() {
	job := &models.Job{
		ID: "j1", AgentID: "agent-dev", Action: models.JobActionPlan,
		Repo: "acme/infra", PRNumber: 1, StackName: "a", Status: models.JobStatusRunning,
	}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.publisher.EXPECT().WriteComment(gomock.Any(), "acme/infra", 1, gomock.Any()).Return(errors.New("scm"))
	require.NoError(s.T(), s.result.Complete(context.Background(), "agent-dev", "j1", true, "out", ""))
}

func (s *JobServicesSuite) TestCompleteUnknownAction() {
	job := &models.Job{
		ID: "j1", AgentID: "agent-dev", Action: models.JobAction("nope"),
		Repo: "acme/infra", PRNumber: 1, StackName: "a", Status: models.JobStatusRunning,
	}
	s.jobs.EXPECT().Get(gomock.Any(), "j1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	require.NoError(s.T(), s.result.Complete(context.Background(), "agent-dev", "j1", true, "", ""))
}

func (s *JobServicesSuite) TestFailExpiredError() {
	s.jobs.EXPECT().FailExpiredLeases(gomock.Any(), gomock.Any()).Return(nil, errors.New("db"))
	require.Error(s.T(), s.result.FailExpired(context.Background()))
}

func (s *JobServicesSuite) TestFailExpiredLockReleaseError() {
	expired := []models.Job{{
		ID: "j1", Action: models.JobActionApply, Repo: "acme/infra", PRNumber: 1, StackName: "a",
		ErrorMsg: "expired",
	}}
	s.jobs.EXPECT().FailExpiredLeases(gomock.Any(), gomock.Any()).Return(expired, nil)
	s.locks.EXPECT().Delete(gomock.Any(), "acme/infra", "a", "default").Return(errors.New("lock"))
	s.publisher.EXPECT().WriteComment(gomock.Any(), "acme/infra", 1, gomock.Any()).Return(nil)
	require.NoError(s.T(), s.result.FailExpired(context.Background()))
}

func (s *JobServicesSuite) TestClaimNegativeWait() {
	s.jobs.EXPECT().ClaimNext(gomock.Any(), "agent-dev", time.Hour).Return(nil, nil)
	job, err := s.claim.Claim(context.Background(), "agent-dev", -time.Second)
	require.NoError(s.T(), err)
	require.Nil(s.T(), job)
}

func (s *JobServicesSuite) TestLeaseReaperDefaultInterval() {
	require.NotNil(s.T(), services.NewLeaseReaper(log.Noop(), s.result, 0))
}

func (s *JobServicesSuite) TestLeaseReaperFailExpiredError() {
	results := services.NewJobResultService(log.Noop(), s.jobs, s.locks, s.publisher)
	reaper := services.NewLeaseReaper(log.Noop(), results, 10*time.Millisecond)
	s.jobs.EXPECT().FailExpiredLeases(gomock.Any(), gomock.Any()).Return(nil, errors.New("db")).MinTimes(1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- reaper.Run(ctx) }()
	time.Sleep(40 * time.Millisecond)
	cancel()
	require.NoError(s.T(), <-done)
}

func (s *JobServicesSuite) TestLeaseReaperFromConfig() {
	results := services.NewJobResultService(log.Noop(), s.jobs, s.locks, s.publisher)
	reaper := services.NewLeaseReaperFromConfig(log.Noop(), results, &config.Config{OrchestratorLeaseReaperInterval: time.Hour})
	require.NotNil(s.T(), reaper)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- reaper.Run(ctx) }()
	cancel()
	require.NoError(s.T(), <-done)
}
