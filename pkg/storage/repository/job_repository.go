package repository

import (
	"context"
	"time"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

//go:generate mockgen -source=job_repository.go -destination=mock_repository/mock_job_repository.go -package=mock_repository

type JobRepository interface {
	Create(ctx context.Context, job *models.Job) error
	Get(ctx context.Context, jobID string) (*models.Job, error)
	ClaimPendingJobForAgent(ctx context.Context, agentID string, status models.JobStatus, leaseExpiresAt *time.Time) (*models.Job, error)
	UpsertPendingJob(ctx context.Context, repo string, prNumber int, stackName string, action string, payload map[string]interface{}, agent string) (*models.Job, error)
	// WithTransaction runs fn in one database transaction. UpsertPendingJob
	// joins that transaction, so none of its rows are visible until fn returns nil.
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
	ReleaseClaimedJob(ctx context.Context, jobID string) error
	FailClaimedJob(ctx context.Context, jobID, errMsg string) error
	ReapExpiredClaims(ctx context.Context, now time.Time) (*ReapExpiredClaimsResult, error)
	Update(ctx context.Context, job *models.Job) error
	UpdatePayload(ctx context.Context, job *models.Job, payload map[string]interface{}) error
	Delete(ctx context.Context, jobID string) error
	DeleteByRepoPRAndStacks(ctx context.Context, repo string, prNumber int, stackNames []string) (int, error)
	RefreshAgentClaims(ctx context.Context, agentID string, leaseExpiresAt *time.Time) error
	CleanupExpiredJobs(ctx context.Context, cutoff time.Time) (int, error)
	ListByRepoPRAction(ctx context.Context, repo string, prNumber int, action models.JobAction) ([]*models.Job, error)
	// WithPlanStatusLock runs fn while this orchestrator holds the plan status
	// lock for repo and sha. The lock is a database row, so a second
	// orchestrator waits until fn returns.
	WithPlanStatusLock(ctx context.Context, repo, sha string, fn func(ctx context.Context) error) error
}

// ReapExpiredClaimsResult summarizes jobs affected by ReapExpiredClaims.
type ReapExpiredClaimsResult struct {
	ClaimedReturned int
	RunningFailed   []*models.Job
}
