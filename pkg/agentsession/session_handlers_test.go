package agentsession

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type commitStubJobService struct {
	err   error
	calls []commitCall
}

type commitCall struct {
	jobID  string
	result string
	output string
	errMsg string
}

func (s *commitStubJobService) CreatePendingJobs(context.Context, *scm.Webhook) error { return nil }
func (s *commitStubJobService) ClaimPendingJob(context.Context, string) (*command.Command, error) {
	return nil, nil
}
func (s *commitStubJobService) ReleaseClaim(context.Context, string) error { return nil }
func (s *commitStubJobService) FailClaimedJob(context.Context, string, string) error {
	return nil
}
func (s *commitStubJobService) ReapExpiredClaims(context.Context) error          { return nil }
func (s *commitStubJobService) RefreshAgentClaims(context.Context, string) error { return nil }
func (s *commitStubJobService) AckJob(context.Context, string, string) error     { return nil }
func (s *commitStubJobService) CommitJobResult(_ context.Context, jobID, _ string, result, output, errMsg string) error {
	s.calls = append(s.calls, commitCall{jobID: jobID, result: result, output: output, errMsg: errMsg})
	return s.err
}

var _ services.JobService = (*commitStubJobService)(nil)

type ackStubJobService struct {
	err   error
	acked []string
}

func (s *ackStubJobService) CreatePendingJobs(context.Context, *scm.Webhook) error { return nil }
func (s *ackStubJobService) ClaimPendingJob(context.Context, string) (*command.Command, error) {
	return nil, nil
}
func (s *ackStubJobService) ReleaseClaim(context.Context, string) error { return nil }
func (s *ackStubJobService) FailClaimedJob(context.Context, string, string) error {
	return nil
}
func (s *ackStubJobService) ReapExpiredClaims(context.Context) error          { return nil }
func (s *ackStubJobService) RefreshAgentClaims(context.Context, string) error { return nil }
func (s *ackStubJobService) AckJob(_ context.Context, jobID, _ string) error {
	s.acked = append(s.acked, jobID)
	return s.err
}
func (s *ackStubJobService) CommitJobResult(context.Context, string, string, string, string, string) error {
	return nil
}

var _ services.JobService = (*ackStubJobService)(nil)

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

func (s *SessionHandlersSuite) TestHandleAckDelegatesToJobService() {
	stub := &ackStubJobService{}
	s.sess.jobService = stub

	err := s.sess.handleAck(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Ack{Ack: &terraplanev1.Ack{Message: "plan accepted"}},
	})
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"job-1"}, stub.acked)
}

func (s *SessionHandlersSuite) TestHandleAckPropagatesError() {
	s.sess.jobService = &ackStubJobService{err: errors.New("ack failed")}

	err := s.sess.handleAck(context.Background(), &terraplanev1.TerraformEnvelope{JobId: "job-1"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "ack failed")
}

func (s *SessionHandlersSuite) TestHandlePlanResultDelegatesSuccess() {
	stub := &commitStubJobService{}
	s.sess.jobService = stub

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true, Output: "plan out"},
		},
	})
	require.NoError(s.T(), err)
	require.Equal(s.T(), []commitCall{{
		jobID: "job-1", result: "success", output: "plan out",
	}}, stub.calls)
}

func (s *SessionHandlersSuite) TestHandlePlanResultDelegatesFailure() {
	stub := &commitStubJobService{}
	s.sess.jobService = stub

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: false, Error: "boom"},
		},
	})
	require.NoError(s.T(), err)
	require.Equal(s.T(), []commitCall{{
		jobID: "job-1", result: "failed", errMsg: "boom",
	}}, stub.calls)
}

func (s *SessionHandlersSuite) TestHandlePlanResultNilPayloadDoesNotCommit() {
	stub := &commitStubJobService{}
	s.sess.jobService = stub

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{PlanResult: nil},
	})
	require.EqualError(s.T(), err, "plan result is nil")
	require.Empty(s.T(), stub.calls)
}

func (s *SessionHandlersSuite) TestHandlePlanResultPropagatesCommitError() {
	s.sess.jobService = &commitStubJobService{err: errors.New("db")}

	err := s.sess.handlePlanResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true},
		},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "db")
}

func (s *SessionHandlersSuite) TestHandleApplyResultDelegatesSuccess() {
	stub := &commitStubJobService{}
	s.sess.jobService = stub

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true, Output: "apply out"},
		},
	})
	require.NoError(s.T(), err)
	require.Equal(s.T(), []commitCall{{
		jobID: "job-1", result: "success", output: "apply out",
	}}, stub.calls)
}

func (s *SessionHandlersSuite) TestHandleApplyResultNilPayloadCommitsThenErrors() {
	stub := &commitStubJobService{}
	s.sess.jobService = stub

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{ApplyResult: nil},
	})
	require.EqualError(s.T(), err, "apply result is nil")
	require.Equal(s.T(), []commitCall{{
		jobID: "job-1", result: "failed", errMsg: "apply result is nil",
	}}, stub.calls)
}

func (s *SessionHandlersSuite) TestHandleApplyResultNilPayloadCommitError() {
	s.sess.jobService = &commitStubJobService{err: errors.New("db")}

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{ApplyResult: nil},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "db")
}

func (s *SessionHandlersSuite) TestHandleApplyResultPropagatesCommitError() {
	s.sess.jobService = &commitStubJobService{err: errors.New("db")}

	err := s.sess.handleApplyResult(context.Background(), &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true},
		},
	})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "db")
}

func (s *SessionHandlersSuite) TestID() {
	require.Equal(s.T(), "agent-1", s.sess.ID())
}
