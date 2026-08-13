package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type PlanService interface {
	RunPlan(ctx context.Context, plan command.PlanCommand) error
}

type planService struct {
	logger      log.Logger
	scmProvider scm.Provider
	jobs        repository.JobRepository
}

func NewPlanService(
	logger log.Logger,
	scmProvider scm.Provider,
	jobs repository.JobRepository,
) PlanService {
	return &planService{
		logger:      logger,
		scmProvider: scmProvider,
		jobs:        jobs,
	}
}

func (s *planService) RunPlan(ctx context.Context, plan command.PlanCommand) error {
	s.logger.Info(
		"Starting terraplane plan",
		"repo", plan.Repo,
		"pr", plan.PRNumber,
		"user", plan.TriggerUser,
		"commit", plan.CommitSHA,
		"stacks", plan.Stacks,
		"environments", plan.Environments,
	)

	file, err := s.scmProvider.GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo)
	if err != nil {
		return fmt.Errorf("failed to fetch terraplane.yaml for repository %s at commit %s: %w", plan.Repo, plan.CommitSHA, err)
	}

	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("failed to parse terraplane.yaml for repository %s at commit %s: %w", plan.Repo, plan.CommitSHA, err)
	}

	stacks, err := config.ResolveStacks(plan.Stacks, plan.Environments)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks for repository %s pull request #%d: %w", plan.Repo, plan.PRNumber, err)
	}

	s.logger.Info(
		"Resolved terraplane stacks for plan",
		"repo", plan.Repo,
		"pr", plan.PRNumber,
		"stack_count", len(stacks),
	)

	var enqueued int
	for _, stack := range stacks {
		job, err := s.jobs.UpsertPending(ctx, &models.Job{
			ID:          uuid.NewString(),
			Repo:        plan.Repo,
			PRNumber:    int32(plan.PRNumber),
			StackName:   stack.Name,
			Dir:         stack.Dir,
			CommitSHA:   plan.CommitSHA,
			AgentID:     stack.Agent,
			Action:      models.JobActionPlan,
			PlanFlags:   plan.PlanFlags,
			TriggerUser: plan.TriggerUser,
			Status:      models.JobStatusPending,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue plan job for stack %q in repository %s pull request #%d: %w", stack.Name, plan.Repo, plan.PRNumber, err)
		}

		s.logger.Info(
			"Enqueued plan job",
			"job_id", job.ID,
			"repo", plan.Repo,
			"pr", plan.PRNumber,
			"stack", stack.Name,
			"agent", stack.Agent,
			"dir", stack.Dir,
			"commit", plan.CommitSHA,
		)
		enqueued++
	}

	s.logger.Info(
		"Finished terraplane plan enqueue",
		"repo", plan.Repo,
		"pr", plan.PRNumber,
		"enqueued_stack_count", enqueued,
	)
	return nil
}
