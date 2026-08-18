package services

import (
	"context"
	"encoding/json"
	"errors"
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

const applyLockWorkspace = "default"

type JobService interface {
	CreatePendingJobs(ctx context.Context, webhook *scm.Webhook) error
	ClaimPendingJob(ctx context.Context, agentID string) (*command.Command, error)
	ReleaseClaim(ctx context.Context, jobID string) error
	FailClaimedJob(ctx context.Context, jobID, errMsg string) error
	ReapExpiredClaims(ctx context.Context) error
}

type jobService struct {
	logger         log.Logger
	jobRepository  repository.JobRepository
	lockRepository repository.LockRepository
	scmProvider    scm.Provider
	jobLease       time.Duration
}

func NewJobService(
	logger log.Logger,
	jobRepository repository.JobRepository,
	lockRepository repository.LockRepository,
	scmProvider scm.Provider,
	config *config.Config,
) JobService {
	return &jobService{
		logger:         logger,
		jobRepository:  jobRepository,
		lockRepository: lockRepository,
		scmProvider:    scmProvider,
		jobLease:       config.OrchestratorJobLease,
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

	file, err := j.scmProvider.GetFile("terraplane.yaml", webhook.CommitSHA, webhook.RepositorySlug)
	if err != nil {
		return fmt.Errorf("failed to fetch terraplane.yaml for repository %s at commit %s: %w", webhook.RepositorySlug, webhook.CommitSHA, err)
	}

	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("failed to parse terraplane.yaml for repository %s at commit %s: %w", webhook.RepositorySlug, webhook.CommitSHA, err)
	}

	stackNames, environmentNames, action := j.resolveStacksAndEnvironments(&cmd)

	resolvedStacks, err := config.ResolveStacks(stackNames, environmentNames)
	if err != nil {
		return fmt.Errorf("failed to resolve stacks for repository %s pull request #%d: %w", webhook.RepositorySlug, webhook.PRNumber, err)
	}

	for _, stack := range resolvedStacks {
		if action == string(models.JobActionApply) {
			ok, err := j.acquireApplyLock(ctx, webhook, stack)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
		}

		payload := map[string]interface{}{
			"trigger_user": webhook.TriggeringUser,
			"commit_sha":   webhook.CommitSHA,
			"stack_name":   stack.Name,
			"dir":          stack.Dir,
		}
		if action == string(models.JobActionPlan) {
			payload["plan_flags"] = cmd.Plan.PlanFlags
		}

		job, err := j.jobRepository.UpsertPendingJob(
			ctx, webhook.RepositorySlug,
			webhook.PRNumber,
			stack.Name,
			action,
			payload,
			stack.Agent,
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
			"agent", stack.Agent,
			"job_id", job.ID,
		)
	}

	return nil
}

func (j *jobService) acquireApplyLock(ctx context.Context, webhook *scm.Webhook, stack terraplaneconfig.ResolvedStack) (bool, error) {
	err := j.lockRepository.Create(ctx, &models.ProjectLock{
		Repo:      webhook.RepositorySlug,
		StackName: stack.Name,
		Workspace: applyLockWorkspace,
		Dir:       stack.Dir,
		CommitSHA: webhook.CommitSHA,
		LockedBy:  webhook.TriggeringUser,
		PRNumber:  int32(webhook.PRNumber),
	})
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, repository.ErrLockExists) {
		return false, fmt.Errorf("failed to create lock for stack %q in repository %s: %w", stack.Name, webhook.RepositorySlug, err)
	}

	existing, getErr := j.lockRepository.Get(ctx, webhook.RepositorySlug, stack.Name, applyLockWorkspace)
	if getErr != nil {
		return false, fmt.Errorf("failed to fetch lock for stack %q in repository %s: %w", stack.Name, webhook.RepositorySlug, getErr)
	}
	if existing != nil && existing.PRNumber == int32(webhook.PRNumber) {
		return true, nil
	}

	lockedPR := int32(0)
	lockedBy := ""
	if existing != nil {
		lockedPR = existing.PRNumber
		lockedBy = existing.LockedBy
	}
	j.logger.Warn(
		"Skipping stack because it is locked",
		"repo", webhook.RepositorySlug,
		"pr", webhook.PRNumber,
		"stack", stack.Name,
		"locked_pr", lockedPR,
		"locked_by", lockedBy,
	)
	return false, nil
}

func (j *jobService) ClaimPendingJob(ctx context.Context, agentID string) (*command.Command, error) {
	expires := time.Now().Add(j.jobLease)
	job, err := j.jobRepository.ClaimPendingJobForAgent(ctx, agentID, models.JobStatusClaimed, &expires)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}

	cmd, err := j.commandFromJob(job)
	if err != nil {
		if failErr := j.jobRepository.FailClaimedJob(ctx, job.ID, err.Error()); failErr != nil {
			return nil, fmt.Errorf("%w (also failed to mark job failed: %v)", err, failErr)
		}
		return nil, err
	}
	return &cmd, nil
}

func (j *jobService) ReleaseClaim(ctx context.Context, jobID string) error {
	return j.jobRepository.ReleaseClaimedJob(ctx, jobID)
}

func (j *jobService) FailClaimedJob(ctx context.Context, jobID, errMsg string) error {
	return j.jobRepository.FailClaimedJob(ctx, jobID, errMsg)
}

func (j *jobService) ReapExpiredClaims(ctx context.Context) error {
	n, err := j.jobRepository.ReapExpiredClaims(ctx, time.Now())
	if err != nil {
		return err
	}
	if n > 0 {
		j.logger.Info("Reaped expired claimed jobs", "count", n)
	}
	return nil
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
		plan.JobID = job.ID
		plan.Dir = job.Dir
		return command.Command{Kind: command.KindPlan, Plan: plan}, nil
	case models.JobActionApply:
		apply := command.ApplyCommand{Stacks: stacks}
		apply.Repo = job.Repo
		apply.PRNumber = int(job.PRNumber)
		apply.CommitSHA = job.CommitSHA
		apply.TriggerUser = triggerUser
		apply.Agent = job.AgentID
		apply.JobID = job.ID
		apply.Dir = job.Dir
		return command.Command{Kind: command.KindApply, Apply: apply}, nil
	case models.JobActionUnlock:
		unlock := command.UnlockCommand{Stacks: stacks}
		unlock.Repo = job.Repo
		unlock.PRNumber = int(job.PRNumber)
		unlock.CommitSHA = job.CommitSHA
		unlock.TriggerUser = triggerUser
		unlock.Agent = job.AgentID
		unlock.JobID = job.ID
		unlock.Dir = job.Dir
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
		action = string(models.JobActionPlan)
	}
	if cmd.Kind == command.KindApply {
		stacks = cmd.Apply.Stacks
		environments = cmd.Apply.Environments
		action = string(models.JobActionApply)
	}
	if cmd.Kind == command.KindUnlock {
		stacks = cmd.Unlock.Stacks
		environments = cmd.Unlock.Environments
		action = string(models.JobActionUnlock)
	}
	return stacks, environments, action
}
