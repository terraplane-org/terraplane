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
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
)

type JobServiceSuite struct {
	suite.Suite
	ctrl *gomock.Controller
	scm  *mock_scm.MockProvider
	jobs *mock_repository.MockJobRepository
	svc  services.JobService
}

func TestJobServiceSuite(t *testing.T) {
	suite.Run(t, new(JobServiceSuite))
}

func (s *JobServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.jobs = mock_repository.NewMockJobRepository(s.ctrl)
	s.svc = services.NewJobService(log.Noop(), s.jobs, s.scm, &config.Config{OrchestratorJobLease: time.Minute})
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
	s.jobs.EXPECT().UpsertPendingJob(
		gomock.Any(),
		"acme/infra",
		42,
		stack,
		action,
		map[string]interface{}{
			"trigger_user": "jace",
			"commit_sha":   "abc123",
			"stack_name":   stack,
			"dir":          dir,
		},
		agent,
	).Return(&models.Job{ID: jobID}, nil)
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

func (s *JobServiceSuite) TestApplyUpsertsPendingJobs() {
	wh := webhook("terraplane apply -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", wh.CommitSHA, wh.RepositorySlug).Return(twoStackYAML, nil)
	s.expectUpsert("a", "stacks/a", "apply", "agent-a", "job-a")

	err := s.svc.CreatePendingJobs(context.Background(), wh)
	require.NoError(s.T(), err)
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
	cmds, err := s.svc.ClaimPendingJobs(context.Background(), nil)
	require.NoError(s.T(), err)
	require.Nil(s.T(), cmds)
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
	require.Equal(s.T(), []string{"a"}, cmds[0].Plan.Stacks)
	require.Equal(s.T(), "-target=x", cmds[0].Plan.PlanFlags)
}

func (s *JobServiceSuite) TestClaimPendingJobsApply() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionApply, `{"trigger_user":"jace"}`)}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.NoError(s.T(), err)
	require.Equal(s.T(), command.KindApply, cmds[0].Kind)
	require.Equal(s.T(), "agent-a", cmds[0].Apply.Agent)
	require.Equal(s.T(), []string{"a"}, cmds[0].Apply.Stacks)
}

func (s *JobServiceSuite) TestClaimPendingJobsUnlock() {
	s.expectClaim([]*models.Job{claimedJob(models.JobAction("unlock"), `{"trigger_user":"jace"}`)}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.NoError(s.T(), err)
	require.Equal(s.T(), command.KindUnlock, cmds[0].Kind)
	require.Equal(s.T(), "agent-a", cmds[0].Unlock.Agent)
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

func (s *JobServiceSuite) TestClaimPendingJobsInvalidPayload() {
	s.expectClaim([]*models.Job{claimedJob(models.JobActionPlan, "{")}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "invalid payload")
	require.Nil(s.T(), cmds)
}

func (s *JobServiceSuite) TestClaimPendingJobsUnknownAction() {
	s.expectClaim([]*models.Job{claimedJob(models.JobAction("nope"), `{}`)}, nil)

	cmds, err := s.svc.ClaimPendingJobs(context.Background(), []string{"agent-a"})
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "unknown job action")
	require.Nil(s.T(), cmds)
}
