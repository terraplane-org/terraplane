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
	Update(ctx context.Context, job *models.Job) error
	Delete(ctx context.Context, jobID string) error
	DeleteByRepoPRAndStacks(ctx context.Context, repo string, prNumber int, stackNames []string) (int, error)

	// UpsertPending creates a pending job or updates the existing pending row for
	// (repo, stack_name, action) with fresher command fields.
	UpsertPending(ctx context.Context, job *models.Job) (*models.Job, error)

	// GetPending returns the pending job for (repo, stack, action), or nil if none.
	GetPending(ctx context.Context, repo, stackName string, action models.JobAction) (*models.Job, error)

	// ClaimNext claims the oldest pending job for agentID whose (repo, stack) has
	// no running job. Returns nil, nil when nothing is available.
	ClaimNext(ctx context.Context, agentID string, lease time.Duration) (*models.Job, error)

	// FailExpiredLeases marks running jobs with expired leases as failed.
	FailExpiredLeases(ctx context.Context, now time.Time) ([]models.Job, error)
}
