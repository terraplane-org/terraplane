package services

import (
	"context"
	"fmt"

	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type JobService interface {
	CreatePendingJobs(ctx context.Context, webhook *scm.Webhook) error
}

type jobService struct {
	logger        log.Logger
	jobRepository repository.JobRepository
	scmProvider   scm.Provider
}

func NewJobService(
	logger log.Logger,
	jobRepository repository.JobRepository,
	scmProvider scm.Provider,
) JobService {
	return &jobService{
		logger:        logger,
		jobRepository: jobRepository,
		scmProvider:   scmProvider,
	}
}

func (j *jobService) CreatePendingJobs(ctx context.Context, webhook *scm.Webhook) error {
	cmd := command.ParseWebhook(webhook)
	if cmd.Kind == command.KindUnknown {
		j.logger.Debug(
			"Ignoring pull request comment that is not a terraplane command",
			"repo", webhook.RepositorySlug,
			"pr", webhook.PRNumber,
			"user", webhook.TriggeringUser,
			"comment", webhook.FullCommand,
		)
		return nil
	}

	// Load the terraplane config from the repository
	file, err := j.scmProvider.GetFile("terraplane.yaml", webhook.CommitSHA, webhook.RepositorySlug)
	if err != nil {
		return fmt.Errorf("failed to fetch terraplane.yaml for repository %s at commit %s: %w", webhook.RepositorySlug, webhook.CommitSHA, err)
	}

	// Parse the terraplane config
	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("failed to parse terraplane.yaml for repository %s at commit %s: %w", webhook.RepositorySlug, webhook.CommitSHA, err)
	}

	// Resolve the stacks and environments for the job
	stackNames, environmentNames, action, err := j.resolveStacksAndEnvironments(&cmd)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks and environments for repository %s pull request #%d: %w", webhook.RepositorySlug, webhook.PRNumber, err)
	}

	resolvedStacks, err := config.ResolveStacks(stackNames, environmentNames)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks for repository %s pull request #%d: %w", webhook.RepositorySlug, webhook.PRNumber, err)
	}

	// Create pending jobs for each stack
	for _, stack := range resolvedStacks {
		agent := stack.Agent
		// Resolve the payload for the job
		payload := map[string]interface{}{
			"trigger_user": webhook.TriggeringUser,
			"commit_sha":   webhook.CommitSHA,
			"stack_name":   stack.Name,
			"dir":          stack.Dir,
		}

		job, err := j.jobRepository.UpsertPendingJob(
			ctx, webhook.RepositorySlug,
			webhook.PRNumber,
			stack.Name,
			action,
			payload,
			agent,
		)
		if err != nil {
			return fmt.Errorf("failed to upsert pending job for repository %s pull request #%d: %w", webhook.RepositorySlug, webhook.PRNumber, err)
		}

		j.logger.Debug(
			"Upserted pending job",
			"repo", webhook.RepositorySlug,
			"pr", webhook.PRNumber,
			"stack", stack.Name,
			"action", action,
			"agent", agent,
			"job_id", job.ID,
		)
	}

	return nil
}

func (j *jobService) resolveStacksAndEnvironments(cmd *command.Command) ([]string, []string, string, error) {
	// TODO: This is awful. We should find a more elegant way to do this
	stacks := []string{}
	environments := []string{}
	action := ""
	if cmd.Kind == command.KindPlan {
		stacks = cmd.Plan.Stacks
		environments = cmd.Plan.Environments
		action = "plan"
	}
	if cmd.Kind == command.KindApply {
		stacks = cmd.Apply.Stacks
		environments = cmd.Apply.Environments
		action = "apply"
	}
	if cmd.Kind == command.KindUnlock {
		stacks = cmd.Unlock.Stacks
		environments = cmd.Unlock.Environments
		action = "unlock"
	}
	return stacks, environments, action, nil
}
