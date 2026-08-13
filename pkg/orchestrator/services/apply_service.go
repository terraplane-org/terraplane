package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type ApplyService interface {
	RunApply(ctx context.Context, apply command.ApplyCommand) error
}

type applyService struct {
	logger      log.Logger
	scmProvider scm.Provider
	jobs        repository.JobRepository
	locks       repository.LockRepository
}

func NewApplyService(
	logger log.Logger,
	scmProvider scm.Provider,
	jobs repository.JobRepository,
	locks repository.LockRepository,
) ApplyService {
	return &applyService{
		logger:      logger,
		scmProvider: scmProvider,
		jobs:        jobs,
		locks:       locks,
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

	var enqueued, skippedLocked int
	for _, stack := range stacks {
		pending, err := s.jobs.GetPending(ctx, apply.Repo, stack.Name, models.JobActionApply)
		if err != nil {
			return fmt.Errorf("failed to look up pending apply for stack %q in repository %s: %w", stack.Name, apply.Repo, err)
		}
		if pending == nil {
			if err := s.locks.Create(ctx, &models.ProjectLock{
				Repo:      apply.Repo,
				StackName: stack.Name,
				Workspace: "default",
				Dir:       stack.Dir,
				CommitSHA: apply.CommitSHA,
				LockedBy:  apply.TriggerUser,
				PRNumber:  int32(apply.PRNumber),
			}); err != nil {
				if errors.Is(err, repository.ErrLockExists) {
					existing, getErr := s.locks.Get(ctx, apply.Repo, stack.Name, "default")
					if getErr != nil {
						return fmt.Errorf("failed to fetch lock for stack %q in repository %s: %w", stack.Name, apply.Repo, getErr)
					}
					s.logLockedStack(apply, stack.Name, existing)
					skippedLocked++
					continue
				}
				return fmt.Errorf("failed to create lock for stack %q in repository %s: %w", stack.Name, apply.Repo, err)
			}
		}

		job, err := s.jobs.UpsertPending(ctx, &models.Job{
			ID:          uuid.NewString(),
			Repo:        apply.Repo,
			PRNumber:    int32(apply.PRNumber),
			StackName:   stack.Name,
			Dir:         stack.Dir,
			CommitSHA:   apply.CommitSHA,
			AgentID:     stack.Agent,
			Action:      models.JobActionApply,
			TriggerUser: apply.TriggerUser,
			Status:      models.JobStatusPending,
		})
		if err != nil {
			if pending == nil {
				if delErr := s.locks.Delete(ctx, apply.Repo, stack.Name, "default"); delErr != nil {
					return fmt.Errorf(
						"failed to enqueue apply job for stack %q in repository %s pull request #%d: %w (also failed to release lock: %v)",
						stack.Name, apply.Repo, apply.PRNumber, err, delErr,
					)
				}
			}
			return fmt.Errorf("failed to enqueue apply job for stack %q in repository %s pull request #%d: %w", stack.Name, apply.Repo, apply.PRNumber, err)
		}

		s.logger.Info(
			"Enqueued apply job",
			"job_id", job.ID,
			"repo", apply.Repo,
			"pr", apply.PRNumber,
			"stack", stack.Name,
			"agent", stack.Agent,
			"dir", stack.Dir,
			"commit", apply.CommitSHA,
		)
		enqueued++
	}

	s.logger.Info(
		"Finished terraplane apply enqueue",
		"repo", apply.Repo,
		"pr", apply.PRNumber,
		"enqueued_stack_count", enqueued,
		"skipped_locked", skippedLocked,
	)
	return nil
}

func (s *applyService) logLockedStack(apply command.ApplyCommand, stackName string, lock *models.ProjectLock) {
	lockedPR := int32(0)
	lockedBy := ""
	if lock != nil {
		lockedPR = lock.PRNumber
		lockedBy = lock.LockedBy
	}
	s.logger.Warn(
		"Skipping stack because it is locked",
		"repo", apply.Repo,
		"pr", apply.PRNumber,
		"stack", stackName,
		"locked_pr", lockedPR,
		"locked_by", lockedBy,
	)
}
