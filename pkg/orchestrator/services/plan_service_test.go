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
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
)

type PlanServiceSuite struct {
	suite.Suite
	ctrl *gomock.Controller
	scm  *mock_scm.MockProvider
	jobs *mock_repository.MockJobRepository
	svc  services.PlanService
}

func TestPlanServiceSuite(t *testing.T) {
	suite.Run(t, new(PlanServiceSuite))
}

func (s *PlanServiceSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.jobs = mock_repository.NewMockJobRepository(s.ctrl)
	s.svc = services.NewPlanService(log.Noop(), s.scm, s.jobs)
}

func (s *PlanServiceSuite) TestFetchConfigFailure() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return("", errors.New("404"))
	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to fetch terraplane.yaml")
}

func (s *PlanServiceSuite) TestParseConfigFailure() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return("stacks: [", nil)
	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to parse terraplane.yaml")
}

func (s *PlanServiceSuite) TestResolveStacksFailure() {
	plan := planCmd("terraplane plan -s missing")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to resolve stacks")
}

func (s *PlanServiceSuite) TestEnqueueAllStacks() {
	plan := planCmd("terraplane plan")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, job *models.Job) (*models.Job, error) {
			require.Equal(s.T(), models.JobActionPlan, job.Action)
			require.Equal(s.T(), models.JobStatusPending, job.Status)
			require.NotEmpty(s.T(), job.AgentID)
			return job, nil
		},
	).Times(2)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}

func (s *PlanServiceSuite) TestEnqueueSelectedStackWithFlags() {
	plan := planCmd("terraplane plan -s a -- -target=module.vpc")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, job *models.Job) (*models.Job, error) {
			require.Equal(s.T(), "a", job.StackName)
			require.Equal(s.T(), "agent-a", job.AgentID)
			require.Equal(s.T(), "-target=module.vpc", job.PlanFlags)
			return job, nil
		},
	)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}

func (s *PlanServiceSuite) TestEnqueueFailure() {
	plan := planCmd("terraplane plan -s a")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoStackYAML, nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.Any()).Return(nil, errors.New("db down"))

	err := s.svc.RunPlan(context.Background(), plan)
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "failed to enqueue plan job")
}

func (s *PlanServiceSuite) TestEnvironmentFlagEnqueuesEnvStacks() {
	plan := planCmd("terraplane plan -e staging")
	s.scm.EXPECT().GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo).Return(twoEnvYAML, nil)
	s.jobs.EXPECT().UpsertPending(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, job *models.Job) (*models.Job, error) {
			require.Contains(s.T(), []string{"a", "b"}, job.StackName)
			return job, nil
		},
	).Times(2)

	err := s.svc.RunPlan(context.Background(), plan)
	require.NoError(s.T(), err)
}
