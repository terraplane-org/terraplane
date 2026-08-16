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
	s.svc = services.NewJobService(log.Noop(), s.jobs, s.scm)
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
