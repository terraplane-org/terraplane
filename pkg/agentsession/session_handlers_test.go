package agentsession

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type SessionHandlersSuite struct {
	suite.Suite
	ctrl  *gomock.Controller
	jobs  *mock_repository.MockJobRepository
	locks *mock_repository.MockLockRepository
	scm   *mock_scm.MockPublisher
	sess  *session
}

func TestSessionHandlersSuite(t *testing.T) {
	suite.Run(t, new(SessionHandlersSuite))
}

func (s *SessionHandlersSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.jobs = mock_repository.NewMockJobRepository(s.ctrl)
	s.locks = mock_repository.NewMockLockRepository(s.ctrl)
	s.scm = mock_scm.NewMockPublisher(s.ctrl)
	s.sess = &session{
		id:             "agent-1",
		logger:         log.Noop(),
		jobRepository:  s.jobs,
		lockRepository: s.locks,
		scmPublisher:   s.scm,
	}
}

func sampleJob() *models.Job {
	return &models.Job{
		ID:        "job-1",
		Repo:      "acme/infra",
		PRNumber:  42,
		StackName: "stg",
		Dir:       "stacks/stg",
		CommitSHA: "abc123",
		Status:    models.JobStatusPending,
	}
}

func (s *SessionHandlersSuite) TestHandleAckMarksJobRunning() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusRunning, updated.Status)
			return nil
		},
	)

	err := s.sess.handleAck(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Ack{Ack: &terraplanev1.Ack{Message: "plan accepted"}},
	})
	require.NoError(s.T(), err)
}

func (s *SessionHandlersSuite) TestHandleAckGetFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))

	err := s.sess.handleAck(context.Background(), &terraplanev1.TerraformEnvelope{JobId: "job-1"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch job")
}

func (s *SessionHandlersSuite) TestHandleAckUpdateFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(sampleJob(), nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))

	err := s.sess.handleAck(context.Background(), &terraplanev1.TerraformEnvelope{JobId: "job-1"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to update job")
}

func (s *SessionHandlersSuite) TestHandlePlanResultSuccessWritesComment() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusSucceeded, updated.Status)
			require.Equal(s.T(), "plan out", updated.Output)
			require.Empty(s.T(), updated.ErrorMsg)
			return nil
		},
	)
	expected := feedback.PlanResultComment(job, true, "plan out", "")
	s.scm.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), expected).Return(nil)

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true, Output: "plan out"},
		},
	})
	require.NoError(s.T(), err)
}

func (s *SessionHandlersSuite) TestHandlePlanResultFailureWritesComment() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusFailed, updated.Status)
			require.Equal(s.T(), "boom", updated.ErrorMsg)
			return nil
		},
	)
	s.scm.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), gomock.Any()).Return(nil)

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: false, Error: "boom"},
		},
	})
	require.NoError(s.T(), err)
}

func (s *SessionHandlersSuite) TestHandlePlanResultCommentFailureIsBestEffort() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.scm.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), gomock.Any()).Return(errors.New("github down"))

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true, Output: "ok"},
		},
	})
	require.NoError(s.T(), err)
}

func (s *SessionHandlersSuite) TestHandlePlanResultNilPayloadDoesNotUpdateJob() {
	// Intention: unlike apply, a nil plan result fails before mutating job state.
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(sampleJob(), nil)

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{PlanResult: nil},
	})
	require.EqualError(s.T(), err, "plan result is nil")
}

func (s *SessionHandlersSuite) TestHandlePlanResultGetFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{JobId: "job-1"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch job")
}

func (s *SessionHandlersSuite) TestHandlePlanResultUpdateFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(sampleJob(), nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true},
		},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to update job")
}

func (s *SessionHandlersSuite) TestHandleApplyResultSuccessReleasesLockAndComments() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusSucceeded, updated.Status)
			require.Equal(s.T(), "apply out", updated.Output)
			return nil
		},
	)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)
	expected := feedback.ApplyResultComment(job, true, "apply out", "")
	s.scm.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), expected).Return(nil)

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true, Output: "apply out"},
		},
	})
	require.NoError(s.T(), err)
}

func (s *SessionHandlersSuite) TestHandleApplyResultFailureStillReleasesLock() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusFailed, updated.Status)
			require.Equal(s.T(), "apply boom", updated.ErrorMsg)
			return nil
		},
	)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)
	s.scm.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), gomock.Any()).Return(nil)

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: false, Error: "apply boom"},
		},
	})
	require.NoError(s.T(), err)
}

func (s *SessionHandlersSuite) TestHandleApplyResultCommentFailureIsBestEffort() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)
	s.scm.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), gomock.Any()).Return(errors.New("github down"))

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true},
		},
	})
	require.NoError(s.T(), err)
}

func (s *SessionHandlersSuite) TestHandleApplyResultNilPayloadMarksFailedReleasesLockThenErrors() {
	// Intention: nil apply result mutates job to failed and releases the lock, then returns an error.
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusFailed, updated.Status)
			require.Equal(s.T(), "apply result is nil", updated.ErrorMsg)
			return nil
		},
	)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{ApplyResult: nil},
	})
	require.EqualError(s.T(), err, "apply result is nil")
}

func (s *SessionHandlersSuite) TestHandleApplyResultUpdateFailureStillAttemptsLockRelease() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true},
		},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to update job")
	require.NotContains(s.T(), err.Error(), "also failed to release lock")
}

func (s *SessionHandlersSuite) TestHandleApplyResultUpdateAndLockReleaseFailure() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(errors.New("unlock failed"))

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true},
		},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "also failed to release lock")
}

func (s *SessionHandlersSuite) TestHandleApplyResultLockReleaseFailureAfterUpdate() {
	job := sampleJob()
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(errors.New("unlock failed"))

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true},
		},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to release lock")
}

func (s *SessionHandlersSuite) TestHandleApplyResultGetFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{JobId: "job-1"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch job")
}

func (s *SessionHandlersSuite) TestID() {
	require.Equal(s.T(), "agent-1", s.sess.ID())
}
