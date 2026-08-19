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
	"github.com/xyzjace/terraplane/pkg/command"
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
	s.svc = services.NewJobService(log.Noop(), s.jobs, s.locks, s.scm, &config.Config{OrchestratorJobLease: time.Minute})
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

func (s *JobServiceSuite) expectClaim(job *models.Job, err error) {
	s.jobs.EXPECT().ClaimPendingJobForAgent(
		gomock.Any(),
		"agent-a",
		models.JobStatusClaimed,
		gomock.Any(),
	).Return(job, err)
}

func (s *JobServiceSuite) requireLeaseAboutOneMinute(lease *time.Time, from time.Time) {
	s.T().Helper()
	require.NotNil(s.T(), lease)
	require.WithinDuration(s.T(), from.Add(time.Minute), *lease, 2*time.Second)
}

func (s *JobServiceSuite) TestClaimPendingJobEmptyAgent() {
	before := time.Now()
	s.jobs.EXPECT().ClaimPendingJobForAgent(
		gomock.Any(),
		"",
		models.JobStatusClaimed,
		gomock.Any(),
	).DoAndReturn(func(_ context.Context, _ string, _ models.JobStatus, lease *time.Time) (*models.Job, error) {
		s.requireLeaseAboutOneMinute(lease, before)
		return nil, nil
	})

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "")
	require.NoError(s.T(), err)
	require.Nil(s.T(), cmd)
}

func (s *JobServiceSuite) TestClaimPendingJobRepositoryError() {
	s.expectClaim(nil, errors.New("db down"))

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Nil(s.T(), cmd)
}

func (s *JobServiceSuite) TestClaimPendingJobPlan() {
	s.expectClaim(claimedJob(models.JobActionPlan, `{"trigger_user":"jace","plan_flags":"-target=x"}`), nil)

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Equal(s.T(), command.KindPlan, cmd.Kind)
	require.Equal(s.T(), "acme/infra", cmd.Plan.Repo)
	require.Equal(s.T(), 42, cmd.Plan.PRNumber)
	require.Equal(s.T(), "abc123", cmd.Plan.CommitSHA)
	require.Equal(s.T(), "jace", cmd.Plan.TriggerUser)
	require.Equal(s.T(), "agent-a", cmd.Plan.Agent)
	require.Equal(s.T(), "job-1", cmd.Plan.JobID)
	require.Equal(s.T(), "stacks/a", cmd.Plan.Dir)
	require.Equal(s.T(), []string{"a"}, cmd.Plan.Stacks)
	require.Equal(s.T(), "-target=x", cmd.Plan.PlanFlags)
}

func (s *JobServiceSuite) TestClaimPendingJobApply() {
	s.expectClaim(claimedJob(models.JobActionApply, `{"trigger_user":"jace"}`), nil)

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Equal(s.T(), command.KindApply, cmd.Kind)
	require.Equal(s.T(), "agent-a", cmd.Apply.Agent)
	require.Equal(s.T(), "stacks/a", cmd.Apply.Dir)
	require.Equal(s.T(), []string{"a"}, cmd.Apply.Stacks)
}

func (s *JobServiceSuite) TestClaimPendingJobUnlock() {
	s.expectClaim(claimedJob(models.JobActionUnlock, `{"trigger_user":"jace"}`), nil)

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Equal(s.T(), command.KindUnlock, cmd.Kind)
	require.Equal(s.T(), "agent-a", cmd.Unlock.Agent)
	require.Equal(s.T(), "stacks/a", cmd.Unlock.Dir)
}

func (s *JobServiceSuite) TestClaimPendingJobEmptyPayload() {
	s.expectClaim(claimedJob(models.JobActionPlan, ""), nil)

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "", cmd.Plan.TriggerUser)
	require.Equal(s.T(), "", cmd.Plan.PlanFlags)
}

func (s *JobServiceSuite) TestClaimPendingJobPayloadNonStringFields() {
	s.expectClaim(claimedJob(models.JobActionPlan, `{"trigger_user":1,"plan_flags":null}`), nil)

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "", cmd.Plan.TriggerUser)
	require.Equal(s.T(), "", cmd.Plan.PlanFlags)
}

func (s *JobServiceSuite) TestClaimPendingJobInvalidPayloadMarksFailed() {
	s.expectClaim(claimedJob(models.JobActionPlan, "{"), nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(nil)

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "invalid payload")
	require.Nil(s.T(), cmd)
}

func (s *JobServiceSuite) TestClaimPendingJobInvalidPayloadFailErrorIsWrapped() {
	s.expectClaim(claimedJob(models.JobActionPlan, "{"), nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(errors.New("db"))

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "invalid payload")
	require.Contains(s.T(), err.Error(), "also failed to mark job failed")
	require.Nil(s.T(), cmd)
}

func (s *JobServiceSuite) TestClaimPendingJobUnknownActionMarksFailed() {
	s.expectClaim(claimedJob(models.JobAction("nope"), `{}`), nil)
	s.jobs.EXPECT().FailClaimedJob(gomock.Any(), "job-1", gomock.Any()).Return(nil)

	cmd, err := s.svc.ClaimPendingJob(context.Background(), "agent-a")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "unknown job action")
	require.Nil(s.T(), cmd)
}

func (s *JobServiceSuite) TestReleaseClaim() {
	s.jobs.EXPECT().ReleaseClaimedJob(gomock.Any(), "job-1").Return(nil)
	require.NoError(s.T(), s.svc.ReleaseClaim(context.Background(), "job-1"))
}

func (s *JobServiceSuite) TestReleaseClaimError() {
	s.jobs.EXPECT().ReleaseClaimedJob(gomock.Any(), "job-1").Return(errors.New("db"))
	err := s.svc.ReleaseClaim(context.Background(), "job-1")
	require.Error(s.T(), err)
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

func (s *JobServiceSuite) TestRefreshAgentClaims() {
	before := time.Now()
	s.jobs.EXPECT().RefreshAgentClaims(gomock.Any(), "agent-a", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, lease *time.Time) error {
			s.requireLeaseAboutOneMinute(lease, before)
			return nil
		},
	)
	require.NoError(s.T(), s.svc.RefreshAgentClaims(context.Background(), "agent-a"))
}

func (s *JobServiceSuite) TestRefreshAgentClaimsError() {
	s.jobs.EXPECT().RefreshAgentClaims(gomock.Any(), "agent-a", gomock.Any()).Return(errors.New("db"))
	err := s.svc.RefreshAgentClaims(context.Background(), "agent-a")
	require.Error(s.T(), err)
}

func (s *JobServiceSuite) TestAckJob() {
	job := claimedJob(models.JobActionPlan, `{}`)
	job.Status = models.JobStatusClaimed
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(s.T(), models.JobStatusRunning, updated.Status)
			return nil
		},
	)
	require.NoError(s.T(), s.svc.AckJob(context.Background(), "job-1"))
}

func (s *JobServiceSuite) TestAckJobGetFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))
	err := s.svc.AckJob(context.Background(), "job-1")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch job")
}

func (s *JobServiceSuite) TestAckJobUpdateFailure() {
	s.jobs.EXPECT().Get(gomock.Any(), "job-1").Return(claimedJob(models.JobActionPlan, `{}`), nil)
	s.jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errors.New("db"))
	err := s.svc.AckJob(context.Background(), "job-1")
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to update job")
}
