package services

import (
	"context"
	"fmt"

	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type UnlockService interface {
	RunUnlock(ctx context.Context, unlock command.UnlockCommand) error
}

type unlockService struct {
	logger        log.Logger
	agentRegistry agentsession.Registry
	scmProvider   scm.Provider
	jobs          repository.JobRepository
	locks         repository.LockRepository
}

func NewUnlockService(
	logger log.Logger,
	agentRegistry agentsession.Registry,
	scmProvider scm.Provider,
	jobs repository.JobRepository,
	locks repository.LockRepository,
) UnlockService {
	return &unlockService{
		logger:        logger,
		agentRegistry: agentRegistry,
		scmProvider:   scmProvider,
		jobs:          jobs,
		locks:         locks,
	}
}

func (s *unlockService) RunUnlock(ctx context.Context, unlock command.UnlockCommand) error {
	s.logger.Info(
		"Starting terraplane unlock",
		"repo", unlock.Repo,
		"pr", unlock.PRNumber,
		"user", unlock.TriggerUser,
		"commit", unlock.CommitSHA,
		"stacks", unlock.Stacks,
	)

	file, err := s.scmProvider.GetFile("terraplane.yaml", unlock.CommitSHA, unlock.Repo)
	if err != nil {
		return fmt.Errorf("failed to fetch terraplane.yaml for repository %s at commit %s: %w", unlock.Repo, unlock.CommitSHA, err)
	}

	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("failed to parse terraplane.yaml for repository %s at commit %s: %w", unlock.Repo, unlock.CommitSHA, err)
	}

	stacks, err := config.ResolveStacks(unlock.Stacks)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks for repository %s pull request #%d: %w", unlock.Repo, unlock.PRNumber, err)
	}

	stackNames := make([]string, len(stacks))
	for i, stack := range stacks {
		stackNames[i] = stack.Name
	}

	s.logger.Info(
		"Resolved terraplane stacks for unlock",
		"repo", unlock.Repo,
		"pr", unlock.PRNumber,
		"stack_count", len(stacks),
		"stacks", stackNames,
	)

	// Locks are keyed by repo + stack + workspace, not PR — release the resolved stacks in this repo.
	deletedLocks, err := s.locks.DeleteByRepoAndStacks(ctx, unlock.Repo, stackNames)
	if err != nil {
		return fmt.Errorf(
			"failed to delete project locks for repository %s stacks %v: %w",
			unlock.Repo, stackNames, err,
		)
	}

	deletedJobs, err := s.jobs.DeleteByRepoPRAndStacks(ctx, unlock.Repo, unlock.PRNumber, stackNames)
	if err != nil {
		return fmt.Errorf(
			"failed to delete jobs for repository %s pull request #%d stacks %v: %w",
			unlock.Repo, unlock.PRNumber, stackNames, err,
		)
	}

	s.logger.Info(
		"Finished terraplane unlock",
		"repo", unlock.Repo,
		"pr", unlock.PRNumber,
		"resolved_stack_count", len(stacks),
		"deleted_lock_count", deletedLocks,
		"deleted_job_count", deletedJobs,
	)

	return nil
}
