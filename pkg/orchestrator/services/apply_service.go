package services

import (
	"context"
	"fmt"

	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type ApplyService interface {
	RunApply(ctx context.Context, apply command.ApplyCommand) error
}

type applyService struct {
	logger        log.Logger
	agentRegistry agentsession.Registry
	scmProvider   scm.Provider
	jobs          repository.JobRepository
}

func NewApplyService(
	logger log.Logger,
	agentRegistry agentsession.Registry,
	scmProvider scm.Provider,
	jobs repository.JobRepository,
) ApplyService {
	return &applyService{
		logger:        logger,
		agentRegistry: agentRegistry,
		scmProvider:   scmProvider,
		jobs:          jobs,
	}
}

func (s *applyService) RunApply(ctx context.Context, apply command.ApplyCommand) error {
	s.logger.Info(
		"Starting terraplane apply",
		"repo", apply.Repo,
		"pr", apply.PRNumber,
		"user", apply.TriggerUser,
		"commit", apply.CommitSHA,
	)

	s.logger.Debug(
		"Fetching repository terraplane config from SCM",
		"repo", apply.Repo,
		"commit", apply.CommitSHA,
		"file", "terraplane.yaml",
	)

	// TODO: Should we cache this somewhere? In the db? On the FS?
	file, err := s.scmProvider.GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo)
	if err != nil {
		return fmt.Errorf("failed to fetch terraplane.yaml for repository %s at commit %s: %w", apply.Repo, apply.CommitSHA, err)
	}

	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("failed to parse terraplane.yaml for repository %s at commit %s: %w", apply.Repo, apply.CommitSHA, err)
	}

	stacks, err := config.ResolveStacks(apply.Stacks)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks for repository %s pull request #%d: %w", apply.Repo, apply.PRNumber, err)
	}

	s.logger.Info(
		"Resolved terraplane stacks for plan",
		"repo", apply.Repo,
		"pr", apply.PRNumber,
		"stack_count", len(stacks),
	)

	var dispatched int
	for _, stack := range stacks {
		s.logger.Debug(
			"Looking up agent session for stack",
			"repo", apply.Repo,
			"pr", apply.PRNumber,
			"stack", stack.Name,
			"agent", stack.Agent,
			"dir", stack.Dir,
		)

		agent, err := s.agentRegistry.Get(ctx, stack.Agent)
		if err != nil {
			return fmt.Errorf("failed to look up agent session %q for stack %q in repository %s: %w", stack.Agent, stack.Name, apply.Repo, err)
		}
		if agent == nil {
			s.logger.Warn(
				"Skipping stack because the configured agent is not connected",
				"repo", apply.Repo,
				"pr", apply.PRNumber,
				"stack", stack.Name,
				"agent", stack.Agent,
			)
			continue
		}

		jobID, err := newJobID()
		if err != nil {
			return fmt.Errorf("failed to generate job ID for stack %q in repository %s: %w", stack.Name, apply.Repo, err)
		}

		if err := s.jobs.Create(ctx, &models.Job{
			ID:        jobID,
			Repo:      apply.Repo,
			PRNumber:  int32(apply.PRNumber),
			StackName: stack.Name,
			Dir:       stack.Dir,
			CommitSHA: apply.CommitSHA,
			Status:    models.JobStatusPending,
		}); err != nil {
			return fmt.Errorf("failed to create job for stack %q in repository %s pull request #%d: %w", stack.Name, apply.Repo, apply.PRNumber, err)
		}

		s.logger.Info(
			"Dispatching apply command to agent",
			"job_id", jobID,
			"repo", apply.Repo,
			"pr", apply.PRNumber,
			"stack", stack.Name,
			"agent", stack.Agent,
			"dir", stack.Dir,
			"commit", apply.CommitSHA,
		)

		if err := agent.Write(ctx, &terraplanev1.TerraformEnvelope{
			JobId: jobID,
			Payload: &terraplanev1.TerraformEnvelope_Apply{
				Apply: &terraplanev1.ApplyCommand{
					Repo:       apply.Repo,
					PrNumber:   int32(apply.PRNumber),
					CommitHash: apply.CommitSHA,
				},
			},
		}); err != nil {
			return fmt.Errorf("failed to dispatch apply command to agent %q for stack %q in repository %s pull request #%d: %w", stack.Agent, stack.Name, apply.Repo, apply.PRNumber, err)
		}

		dispatched++
	}

	if dispatched == 0 {
		s.logger.Warn(
			"Apply finished without dispatching any stacks because no configured agents were connected",
			"repo", apply.Repo,
			"pr", apply.PRNumber,
			"stack_count", len(stacks),
		)
		return nil
	}

	s.logger.Info(
		"Finished terraplane apply dispatch",
		"repo", apply.Repo,
		"pr", apply.PRNumber,
		"dispatched_stack_count", dispatched,
		"resolved_stack_count", len(stacks),
	)

	return nil
}
