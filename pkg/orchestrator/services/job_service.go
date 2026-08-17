package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type JobService interface {
	CreatePendingJobs(ctx context.Context, webhook *scm.Webhook) error
	ClaimPendingJobs(ctx context.Context, agents []string) ([]command.Command, error)
}

type jobService struct {
	logger        log.Logger
	jobRepository repository.JobRepository
	scmProvider   scm.Provider
	jobLease      time.Duration
}

func NewJobService(
	logger log.Logger,
	jobRepository repository.JobRepository,
	scmProvider scm.Provider,
	config *config.Config,
) JobService {
	return &jobService{
		logger:        logger,
		jobRepository: jobRepository,
		scmProvider:   scmProvider,
		jobLease:      config.OrchestratorJobLease,
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

	stackNames, environmentNames, action := j.resolveStacksAndEnvironments(&cmd)

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

func (j *jobService) ClaimPendingJobs(ctx context.Context, agents []string) ([]command.Command, error) {
	if len(agents) == 0 {
		return nil, nil
	}

	expires := time.Now().Add(j.jobLease)
	jobs, err := j.jobRepository.ClaimPendingJobsForAgents(ctx, agents, models.JobStatusClaimed, &expires)
	if err != nil {
		return nil, err
	}

	// Rehydrate jobs in to command objects
	commands := make([]command.Command, len(jobs))
	for i, job := range jobs {
		commands[i], err = j.commandFromJob(job)
		if err != nil {
			return nil, err
		}
	}
	return commands, nil
}

func (j *jobService) commandFromJob(job *models.Job) (command.Command, error) {
	payload, err := unmarshalJobPayload(job.Payload)
	if err != nil {
		return command.Command{}, fmt.Errorf("invalid payload for job %s: %w", job.ID, err)
	}

	stacks := []string{job.StackName}
	triggerUser := payloadString(payload, "trigger_user")

	switch job.Action {
	case models.JobActionPlan:
		plan := command.PlanCommand{
			Stacks:    stacks,
			PlanFlags: payloadString(payload, "plan_flags"),
		}
		plan.Repo = job.Repo
		plan.PRNumber = int(job.PRNumber)
		plan.CommitSHA = job.CommitSHA
		plan.TriggerUser = triggerUser
		plan.Agent = job.AgentID
		return command.Command{Kind: command.KindPlan, Plan: plan}, nil
	case models.JobActionApply:
		apply := command.ApplyCommand{Stacks: stacks}
		apply.Repo = job.Repo
		apply.PRNumber = int(job.PRNumber)
		apply.CommitSHA = job.CommitSHA
		apply.TriggerUser = triggerUser
		apply.Agent = job.AgentID
		return command.Command{Kind: command.KindApply, Apply: apply}, nil
	case models.JobAction("unlock"):
		unlock := command.UnlockCommand{Stacks: stacks}
		unlock.Repo = job.Repo
		unlock.PRNumber = int(job.PRNumber)
		unlock.CommitSHA = job.CommitSHA
		unlock.TriggerUser = triggerUser
		unlock.Agent = job.AgentID
		return command.Command{Kind: command.KindUnlock, Unlock: unlock}, nil
	default:
		return command.Command{}, fmt.Errorf("unknown job action: %s", job.Action)
	}
}

func unmarshalJobPayload(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func payloadString(payload map[string]any, key string) string {
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func (j *jobService) resolveStacksAndEnvironments(cmd *command.Command) ([]string, []string, string) {
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
	return stacks, environments, action
}
