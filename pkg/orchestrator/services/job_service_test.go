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
	"github.com/xyzjace/terraplane/pkg/agentapi"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
)

type JobServiceSuite struct {
	suite.Suite
	ctrl  *gomock.Controller
	scm   *mock_scm.MockProvider
	jobs  *mock_repository.MockJobRepository
	locks *mock_repository.MockLockRepository
	pub   *mock_scm.MockPublisher
	svc   services.JobService
}

func TestJobServiceSuite(t *testing.T) {
	suite.Run(t, new(JobServiceSuite))
}

func (s *JobServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.jobs = mock_repository.NewMockJobRepository(s.ctrl)
	s.locks = mock_repository.NewMockLockRepository(s.ctrl)
	s.pub = mock_scm.NewMockPublisher(s.ctrl)
	s.svc = services.NewJobService(log.Noop(), s.jobs, s.locks, s.scm, s.pub, &config.Config{OrchestratorJobLease: time.Minute})
}

func webhook(comment string) *scm.Webhook {
	return &scm.Webhook{
		RepositorySlug: "acme/infra",
		PRNumber:       42,
		FullCommand:    comment,
		TriggeringUser: "jace",
		CommitSHA:      "abc123",
	}
}

func (s *JobServiceSuite) expectUpsert(stack, dir, action, agent, jobID string) {
	payload := map[string]interface{}{
		"trigger_user": "jace",
		"commit_sha":   "abc123",
		"stack_name":   stack,
		"dir":          dir,
	}
	if action == string(models.JobActionPlan) {
		payload["plan_flags"] = ""
	}
	s.jobs.EXPECT().UpsertPendingJob(
		gomock.Any(),
		"acme/infra",
		42,
		stack,
		action,
		payload,
		agent,
	).Return(&models.Job{ID: jobID}, nil)
}

func (s *JobServiceSuite) expectLockCreate(stack, dir string, err error) {
	s.locks.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&models.ProjectLock{})).DoAndReturn(
		func(_ context.Context, lock *models.ProjectLock) error {
			require.Equal(s.T(), "acme/infra", lock.Repo)
			require.Equal(s.T(), stack, lock.StackName)
			require.Equal(s.T(), "default", lock.Workspace)
			require.Equal(s.T(), dir, lock.Dir)
			require.Equal(s.T(), "abc123", lock.CommitSHA)
			require.Equal(s.T(), "jace", lock.LockedBy)
			require.Equal(s.T(), int32(42), lock.PRNumber)
			return err
		},
	)
}

func (s *JobServiceSuite) TestIgnoresUnknownCommands() {
	err := s.svc.CreatePendingJobs(context.Background(), webhook("not a terraplane command"))
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestFetchConfigFailure() {
	wh := webhook("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return("", errors.New("404"))

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch terraplane.yaml")
}

func (s *JobServiceSuite) TestParseConfigFailure() {
	wh := webhook("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return("stacks: [", nil)

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to parse terraplane.yaml")
}

func (s *JobServiceSuite) TestResolveStacksFailure() {
	wh := webhook("terraplane plan -s missing")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to resolve stacks")
}

func (s *JobServiceSuite) TestPlanUpsertsAllStacks() {
	wh := webhook("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectUpsert("a", "stacks/a", "plan", "agent-a", "job-a")
	s.expectUpsert("b", "stacks/b", "plan", "agent-b", "job-b")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestPlanNamedStack() {
	wh := webhook("terraplane plan -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectUpsert("a", "stacks/a", "plan", "agent-a", "job-a")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestPlanEnvironmentFlag() {
	wh := webhook("terraplane plan -e staging")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoEnvYAML, nil)
	s.expectUpsert("a", "stacks/a", "plan", "agent-a", "job-a")
	s.expectUpsert("b", "stacks/b", "plan", "agent-b", "job-b")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestPlanPersistsPlanFlags() {
	wh := webhook("terraplane plan -s a -- -target=module.vpc")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.jobs.EXPECT().UpsertPendingJob(
		gomock.Any(),
		"acme/infra",
		42,
		"a",
		"plan",
		map[string]interface{}{
			"trigger_user": "jace",
			"commit_sha":   "abc123",
			"stack_name":   "a",
			"dir":          "stacks/a",
			"plan_flags":   "-target=module.vpc",
		},
		"agent-a",
	).Return(&models.Job{ID: "job-a"}, nil)

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestApplyCreatesLockThenUpserts() {
	wh := webhook("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectLockCreate("a", "stacks/a", nil)
	s.expectUpsert("a", "stacks/a", "apply", "agent-a", "job-a")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestApplySupersedesWhenLockedBySamePR() {
	wh := webhook("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectLockCreate("a", "stacks/a", repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), "acme/infra", "a", "default").Return(&models.ProjectLock{
		PRNumber: 42,
		LockedBy: "jace",
	}, nil)
	s.expectUpsert("a", "stacks/a", "apply", "agent-a", "job-a")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestApplySkipsWhenLockedByOtherPR() {
	wh := webhook("terraplane apply")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectLockCreate("a", "stacks/a", repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), "acme/infra", "a", "default").Return(&models.ProjectLock{
		PRNumber: 7,
		LockedBy: "other",
	}, nil)
	s.expectLockCreate("b", "stacks/b", nil)
	s.expectUpsert("b", "stacks/b", "apply", "agent-b", "job-b")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestApplySkipsWhenLockExistsAndGetReturnsNil() {
	wh := webhook("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectLockCreate("a", "stacks/a", repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), "acme/infra", "a", "default").Return(nil, nil)

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestApplyLockCreateFailure() {
	wh := webhook("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectLockCreate("a", "stacks/a", errors.New("db"))

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to create lock")
}

func (s *JobServiceSuite) TestApplyLockGetFailure() {
	wh := webhook("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectLockCreate("a", "stacks/a", repository.ErrLockExists)
	s.locks.EXPECT().Get(gomock.Any(), "acme/infra", "a", "default").Return(nil, errors.New("db"))

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch lock")
}

func (s *JobServiceSuite) TestUnlockUpsertsPendingJobs() {
	wh := webhook("terraplane unlock -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectUpsert("a", "stacks/a", "unlock", "agent-a", "job-a")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
}

func (s *JobServiceSuite) TestUpsertFailureAbortsRemainingStacks() {
	wh := webhook("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.jobs.EXPECT().UpsertPendingJob(
		gomock.Any(), "acme/infra", 42, "a", "plan", gomock.Any(), "agent-a",
	).Return(nil, errors.New("db down"))

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to upsert pending job")
}

func claimedJob(action models.JobAction, payload string) *models.Job {
	return &models.Job{
		ID:        "job-1",
		Repo:      "acme/infra",
		PRNumber:  42,
		StackName: "a",
		Dir:       "stacks/a",
		CommitSHA: "abc123",
		AgentID:   "agent-a",
		Action:    action,
		Payload:   payload,
	}
}

func (s *JobServiceSuite) expectClaim(jobs []*models.Job, err error) {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(
		gomock.Any(),
		[]string{"agent-a"},
		models.JobStatusClaimed,
		gomock.Any(),
	).Return(jobs, err)
}

func (s *JobServiceSuite) TestClaimPendingJobsEmptyAgents() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(
		gomock.Any(),
		nil,
		models.JobStatusClaimed,
		gomock.Any(),
	).Return(nil, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), nil)
	require.NoError(s.T(), err)
	require.Empty(s.T(), cmds)
}

func (s *JobServiceSuite) TestClaimPendingJobsRepositoryError() {
	s.expectClaim(nil, errors.New("db down"))

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.Error(s.T(), err)
	require.Nil(s.T(), cmds)
}

func (s *JobServiceSuite) TestClaimPendingJobsPlan() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionPlan, `{"trigger_user":"jace","plan_flags":"-target=x"}`)}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.NoError(s.T(), err)
	require.Len(s.T(), cmds, 1)
	require.Equal(s.T(), command.KindPlan, cmds[0].Kind)
	require.Equal(s.T(), "acme/infra", cmds[0].Plan.Repo)
	require.Equal(s.T(), 42, cmds[0].Plan.PRNumber)
	require.Equal(s.T(), "abc123", cmds[0].Plan.CommitSHA)
	require.Equal(s.T(), "jace", cmds[0].Plan.TriggerUser)
	require.Equal(s.T(), "agent-a", cmds[0].Plan.Agent)
	require.Equal(s.T(), "job-1", cmds[0].Plan.JobID)
	require.Equal(s.T(), "stacks/a", cmds[0].Plan.Dir)
	require.Equal(s.T(), []string{"a"}, cmds[0].Plan.Stacks)
	require.Equal(s.T(), "-target=x", cmds[0].Plan.PlanFlags)
}

func (s *JobServiceSuite) TestClaimPendingJobsApply() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionApply, `{"trigger_user":"jace"}`)}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.NoError(s.T(), err)
	require.Equal(s.T(), command.KindApply, cmds[0].Kind)
	require.Equal(s.T(), "agent-a", cmds[0].Apply.Agent)
	require.Equal(s.T(), "stacks/a", cmds[0].Apply.Dir)
	require.Equal(s.T(), []string{"a"}, cmds[0].Apply.Stacks)
}

func (s *JobServiceSuite) TestClaimPendingJobsUnlock() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionUnlock, `{"trigger_user":"jace"}`)}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.NoError(s.T(), err)
	require.Equal(s.T(), command.KindUnlock, cmds[0].Kind)
	require.Equal(s.T(), "agent-a", cmds[0].Unlock.Agent)
	require.Equal(s.T(), "stacks/a", cmds[0].Unlock.Dir)
}

func (s *JobServiceSuite) TestClaimPendingJobsEmptyPayload() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionPlan, "")}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.NoError(s.T(), err)
	require.Equal(s.T(), "", cmds[0].Plan.TriggerUser)
	require.Equal(s.T(), "", cmds[0].Plan.PlanFlags)
}

func (s *JobServiceSuite) TestClaimPendingJobsPayloadNonStringFields() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionPlan, `{"trigger_user":1,"plan_flags":null}`)}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.NoError(s.T(), err)
	require.Equal(s.T(), "", cmds[0].Plan.TriggerUser)
	require.Equal(s.T(), "", cmds[0].Plan.PlanFlags)
}

func (s *JobServiceSuite) TestClaimPendingJobsInvalidPayloadMarksFailed() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionPlan, "{")}, nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "invalid payload")
	require.Nil(s.T(), cmds)
}

func (s *JobServiceSuite) TestClaimPendingJobsInvalidPayloadFailErrorIsWrapped() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionPlan, "{")}, nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(errors.New("db"))

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "invalid payload")
	require.Contains(s.T(), err.Error(), "also failed to mark job failed")
	require.Nil(s.T(), cmds)
}

func (s *JobServiceSuite) TestClaimPendingJobsUnknownActionMarksFailed() {
	s.expectClaim([]*models.Job{claimedJob(models.JobAction("nope"), `{}`)}, nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "unknown job action")
	require.Nil(s.T(), cmds)
}

func (s *JobServiceSuite) TestFailClaimedJob() {
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", "nope").Return(nil)
	require.NoError(s.T(), s.svc.FailClaimedJob(context.Background(), "job-1", "nope"))
}

func (s *JobServiceSuite) TestReapExpiredClaimsNone() {
	s.jobs.EXPECT().ReapExpiredClaims(gomock.Any(), gomock.Any()).Return(0, nil)
	require.NoError(s.T(), s.svc.ReapExpiredClaims(context.Background()))
}

func (s *JobServiceSuite) TestReapExpiredClaimsSome() {
	s.jobs.EXPECT().ReapExpiredClaims(gomock.Any(), gomock.Any()).Return(2, nil)
	require.NoError(s.T(), s.svc.ReapExpiredClaims(context.Background()))
}

func (s *JobServiceSuite) TestReapExpiredClaimsError() {
	s.jobs.EXPECT().ReapExpiredClaims(gomock.Any(), gomock.Any()).Return(0, errors.New("db"))
	err := s.svc.ReapExpiredClaims(context.Background())
	require.Error(s.T(), err)
}

func (s *JobServiceSuite) TestPollJobEmptyAgent() {
	job, err := s.svc.PollJob(context.Background(), "")
	require.Error(s.T(), err)
	require.Nil(s.T(), job)
}

func (s *JobServiceSuite) TestPollJobNone() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(gomock.Any(), []string{"agent-a"}, models.JobStatusClaimed, gomock.Any()).Return(nil, nil)
	job, err := s.svc.PollJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Nil(s.T(), job)
}

func (s *JobServiceSuite) TestPollJobClaimError() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(gomock.Any(), []string{"agent-a"}, models.JobStatusClaimed, gomock.Any()).Return(nil, errors.New("db"))
	job, err := s.svc.PollJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Nil(s.T(), job)
}

func (s *JobServiceSuite) TestPollJobReturnsPlan() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(gomock.Any(), []string{"agent-a"}, models.JobStatusClaimed, gomock.Any()).Return([]*models.Job{
		claimedJob(models.JobActionPlan, `{"plan_flags":"-target=x"}`),
	}, nil)
	job, err := s.svc.PollJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "job-1", job.JobID)
	require.Equal(s.T(), "plan", job.Action)
	require.Equal(s.T(), "-target=x", job.PlanFlags)
}

func (s *JobServiceSuite) TestPollJobInvalidPayloadMarksFailed() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(gomock.Any(), []string{"agent-a"}, models.JobStatusClaimed, gomock.Any()).Return([]*models.Job{
		claimedJob(models.JobActionPlan, `{`),
	}, nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(nil)
	job, err := s.svc.PollJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Nil(s.T(), job)
}

func (s *JobServiceSuite) TestPollJobReturnsApply() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(gomock.Any(), []string{"agent-a"}, models.JobStatusClaimed, gomock.Any()).Return([]*models.Job{
		claimedJob(models.JobActionApply, `{}`),
	}, nil)
	job, err := s.svc.PollJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "apply", job.Action)
}

func (s *JobServiceSuite) TestPollJobUnsupportedActionMarksFailed() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(gomock.Any(), []string{"agent-a"}, models.JobStatusClaimed, gomock.Any()).Return([]*models.Job{
		claimedJob(models.JobActionUnlock, `{}`),
	}, nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(nil)
	job, err := s.svc.PollJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "unsupported job action")
	require.Nil(s.T(), job)
}

func (s *JobServiceSuite) TestPollJobInvalidPayloadFailErrorWrapped() {
	s.jobs.EXPECT().ClaimPendingJobsForAgents(gomock.Any(), []string{"agent-a"}, models.JobStatusClaimed, gomock.Any()).Return([]*models.Job{
		claimedJob(models.JobActionPlan, `{`),
	}, nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(errors.New("db"))
	job, err := s.svc.PollJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "also failed to mark job failed")
	require.Nil(s.T(), job)
}

func (s *JobServiceSuite) TestAckAndHeartbeat() {
	s.jobs.EXPECT().AckJob(gomock.Any(), "job-1", "agent-a", gomock.Any()).Return(nil)
	require.NoError(s.T(), s.svc.AckJob(context.Background(), "agent-a", "job-1"))
	s.jobs.EXPECT().RenewLease(gomock.Any(), "job-1", "agent-a", gomock.Any()).Return(nil)
	require.NoError(s.T(), s.svc.Heartbeat(context.Background(), "agent-a", "job-1"))
}

func (s *JobServiceSuite) TestRecordPlanResult() {
	job := claimedJob(models.JobActionPlan, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusSucceeded, updated.Status)
			require.Equal(s.T(), "ok", updated.Output)
			return nil
		},
	)
	s.pub.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), feedback.PlanResultComment(job, true, "ok", "")).Return(nil)
	require.NoError(s.T(), s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true, Output: "ok"}))
}

func (s *JobServiceSuite) TestRecordPlanResultFailureCommentBestEffort() {
	job := claimedJob(models.JobActionPlan, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.pub.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), gomock.Any()).Return(errors.New("github"))
	require.NoError(s.T(), s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: false, Error: "boom"}))
}

func (s *JobServiceSuite) TestRecordPlanResultUpdateError() {
	job := claimedJob(models.JobActionPlan, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	err := s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true})
	require.Error(s.T(), err)
}

func (s *JobServiceSuite) TestRecordResultWrongAgent() {
	job := claimedJob(models.JobActionPlan, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	err := s.svc.RecordResult(context.Background(), "other", "job-1", agentapi.Result{Success: true})
	require.ErrorIs(s.T(), err, repository.ErrJobNotFound)
}

func (s *JobServiceSuite) TestRecordResultGetError() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))
	err := s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true})
	require.Error(s.T(), err)
}

func (s *JobServiceSuite) TestRecordResultUnlockRejected() {
	job := claimedJob(models.JobActionUnlock, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	err := s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "unexpected job action")
}

func (s *JobServiceSuite) TestRecordApplyResult() {
	job := claimedJob(models.JobActionApply, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)
	s.pub.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), gomock.Any()).Return(nil)
	require.NoError(s.T(), s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true, Output: "applied"}))
}

func (s *JobServiceSuite) TestRecordApplyResultUpdateAndLockFailure() {
	job := claimedJob(models.JobActionApply, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(errors.New("lock"))
	err := s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true})
	require.Contains(s.T(), err.Error(), "also failed to release lock")
}

func (s *JobServiceSuite) TestRecordApplyResultUpdateFailureReleasesLock() {
	job := claimedJob(models.JobActionApply, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)
	err := s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true})
	require.Error(s.T(), err)
	require.NotContains(s.T(), err.Error(), "also failed")
}

func (s *JobServiceSuite) TestRecordApplyResultLockReleaseFailure() {
	job := claimedJob(models.JobActionApply, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(errors.New("lock"))
	err := s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: true})
	require.Contains(s.T(), err.Error(), "failed to release lock")
}

func (s *JobServiceSuite) TestRecordApplyResultCommentBestEffort() {
	job := claimedJob(models.JobActionApply, `{}`)
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	s.locks.EXPECT().Delete(gomock.Any(), job.Repo, job.StackName, "default").Return(nil)
	s.pub.EXPECT().WriteComment(gomock.Any(), job.Repo, int(job.PRNumber), gomock.Any()).Return(errors.New("github"))
	require.NoError(s.T(), s.svc.RecordResult(context.Background(), "agent-a", "job-1", agentapi.Result{Success: false, Error: "nope"}))
}
