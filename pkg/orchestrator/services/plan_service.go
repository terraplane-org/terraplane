package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type PlanService interface {
	RunPlan(ctx context.Context, plan command.PlanCommand) error
}

type planService struct {
	logger        log.Logger
	agentRegistry agentsession.Registry
	scmProvider   scm.Provider
	jobs          repository.JobRepository
}

func NewPlanService(
	logger log.Logger,
	agentRegistry agentsession.Registry,
	scmProvider scm.Provider,
	jobs repository.JobRepository,
) PlanService {
	return &planService{
		logger:        logger,
		agentRegistry: agentRegistry,
		scmProvider:   scmProvider,
		jobs:          jobs,
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
	)

	s.logger.Debug(
		"Fetching repository terraplane config from SCM",
		"repo", plan.Repo,
		"commit", plan.CommitSHA,
		"file", "terraplane.yaml",
	)

	file, err := s.scmProvider.GetFile("terraplane.yaml", plan.CommitSHA, plan.Repo)
	if err != nil {
		return fmt.Errorf("failed to fetch terraplane.yaml for repository %s at commit %s: %w", plan.Repo, plan.CommitSHA, err)
	}

	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("failed to parse terraplane.yaml for repository %s at commit %s: %w", plan.Repo, plan.CommitSHA, err)
	}

	stacks, err := config.ResolveStacks(plan.Stacks)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks for repository %s pull request #%d: %w", plan.Repo, plan.PRNumber, err)
	}

	s.logger.Info(
		"Resolved terraplane stacks for plan",
		"repo", plan.Repo,
		"pr", plan.PRNumber,
		"stack_count", len(stacks),
	)

	connected, err := anyConnectedAgent(ctx, s.agentRegistry, stacks)
	if err != nil {
		return fmt.Errorf("failed to look up agent sessions for repository %s: %w", plan.Repo, err)
	}
	if !connected {
		s.logger.Warn(
			"Plan finished without dispatching any stacks because none of the agents required by terraplane.yaml are connected",
			"repo", plan.Repo,
			"pr", plan.PRNumber,
			"stack_count", len(stacks),
			"required_agents", uniqueAgentNames(stacks),
		)
		return nil
	}

	var dispatched int
	for _, stack := range stacks {
		s.logger.Debug(
			"Looking up agent session for stack",
			"repo", plan.Repo,
			"pr", plan.PRNumber,
			"stack", stack.Name,
			"agent", stack.Agent,
			"dir", stack.Dir,
		)

		agent, err := s.agentRegistry.Get(ctx, stack.Agent)
		if err != nil {
			return fmt.Errorf("failed to look up agent session %q for stack %q in repository %s: %w", stack.Agent, stack.Name, plan.Repo, err)
		}
		if agent == nil {
			s.logger.Warn(
				"Skipping stack because the configured agent is not connected",
				"repo", plan.Repo,
				"pr", plan.PRNumber,
				"stack", stack.Name,
				"agent", stack.Agent,
			)
			continue
		}

		jobID := newJobID()

		if err := s.jobs.Create(ctx, &models.Job{
			ID:        jobID,
			Repo:      plan.Repo,
			PRNumber:  int32(plan.PRNumber),
			StackName: stack.Name,
			Dir:       stack.Dir,
			CommitSHA: plan.CommitSHA,
			Status:    models.JobStatusPending,
		}); err != nil {
			return fmt.Errorf("failed to create job for stack %q in repository %s pull request #%d: %w", stack.Name, plan.Repo, plan.PRNumber, err)
		}

		s.logger.Info(
			"Dispatching plan command to agent",
			"job_id", jobID,
			"repo", plan.Repo,
			"pr", plan.PRNumber,
			"stack", stack.Name,
			"agent", stack.Agent,
			"dir", stack.Dir,
			"commit", plan.CommitSHA,
		)

		if err := agent.Write(ctx, &terraplanev1.TerraformEnvelope{
			JobId: jobID,
			Payload: &terraplanev1.TerraformEnvelope_Plan{
				Plan: &terraplanev1.PlanCommand{
					Repo:       plan.Repo,
					PrNumber:   int32(plan.PRNumber),
					CommitHash: plan.CommitSHA,
					PlanFlags:  plan.PlanFlags,
					StackName:  stack.Name,
					Dir:        stack.Dir,
				},
			},
		}); err != nil {
			if delErr := s.jobs.Delete(ctx, jobID); delErr != nil {
				return fmt.Errorf(
					"failed to dispatch plan command to agent %q for stack %q in repository %s pull request #%d: %w (also failed to delete job: %v)",
					stack.Agent, stack.Name, plan.Repo, plan.PRNumber, err, delErr,
				)
			}
			return fmt.Errorf("failed to dispatch plan command to agent %q for stack %q in repository %s pull request #%d: %w", stack.Agent, stack.Name, plan.Repo, plan.PRNumber, err)
		}

		dispatched++
	}

	s.logger.Info(
		"Finished terraplane plan dispatch",
		"repo", plan.Repo,
		"pr", plan.PRNumber,
		"dispatched_stack_count", dispatched,
		"resolved_stack_count", len(stacks),
	)

	return nil
}

func newJobID() string {
	return uuid.NewString()
}
