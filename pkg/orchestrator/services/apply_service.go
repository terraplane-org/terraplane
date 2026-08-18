package services

import (
	"context"
	"fmt"

	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
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
}

func NewApplyService(
	logger log.Logger,
	agentRegistry agentsession.Registry,
	scmProvider scm.Provider,
) ApplyService {
	return &applyService{
		logger:        logger,
		agentRegistry: agentRegistry,
		scmProvider:   scmProvider,
	}
}

func (s *applyService) RunApply(ctx context.Context, apply command.ApplyCommand) error {
	s.logger.Info(
		"Starting terraplane apply",
		"repo", apply.Repo,
		"pr", apply.PRNumber,
		"user", apply.TriggerUser,
		"commit", apply.CommitSHA,
		"stacks", apply.Stacks,
		"environments", apply.Environments,
	)

	s.logger.Debug(
		"Fetching repository terraplane config from SCM",
		"repo", apply.Repo,
		"commit", apply.CommitSHA,
		"file", "terraplane.yaml",
	)

	file, err := s.scmProvider.GetFile("terraplane.yaml", apply.CommitSHA, apply.Repo)
	if err != nil {
		return fmt.Errorf("failed to fetch terraplane.yaml for repository %s at commit %s: %w", apply.Repo, apply.CommitSHA, err)
	}

	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("failed to parse terraplane.yaml for repository %s at commit %s: %w", apply.Repo, apply.CommitSHA, err)
	}

	stacks, err := config.ResolveStacks(apply.Stacks, apply.Environments)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks for repository %s pull request #%d: %w", apply.Repo, apply.PRNumber, err)
	}

	s.logger.Info(
		"Resolved terraplane stacks for apply",
		"repo", apply.Repo,
		"pr", apply.PRNumber,
		"stack_count", len(stacks),
	)

	connected, err := anyConnectedAgent(ctx, s.agentRegistry, stacks)
	if err != nil {
		return fmt.Errorf("failed to look up agent sessions for repository %s: %w", apply.Repo, err)
	}
	if !connected {
		s.logger.Warn(
			"Apply finished without dispatching any stacks because none of the agents required by terraplane.yaml are connected",
			"repo", apply.Repo,
			"pr", apply.PRNumber,
			"stack_count", len(stacks),
			"required_agents", uniqueAgentNames(stacks),
		)
		return nil
	}

	var dispatched, skippedNoAgent int
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
			skippedNoAgent++
			continue
		}

		jobID := apply.JobID

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
					StackName:  stack.Name,
					Dir:        stack.Dir,
				},
			},
		}); err != nil {
			return fmt.Errorf("failed to dispatch apply command to agent %q for stack %q in repository %s pull request #%d: %w", stack.Agent, stack.Name, apply.Repo, apply.PRNumber, err)
		}

		dispatched++
	}

	if dispatched == 0 {
		s.logger.Warn(
			"Apply finished without dispatching any stacks",
			"repo", apply.Repo,
			"pr", apply.PRNumber,
			"stack_count", len(stacks),
			"skipped_no_agent", skippedNoAgent,
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
