package repository

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

//go:generate mockgen -source=job_repository.go -destination=mock_repository/mock_job_repository.go -package=mock_repository

type JobRepository interface {
	Create(ctx context.Context, job *models.Job) error
	Get(ctx context.Context, jobID string) (*models.Job, error)
	GetPendingJobsForAgents(ctx context.Context, agents []string) ([]*models.Job, error)
	UpsertPendingJob(ctx context.Context, repo string, prNumber int, stackName string, action string, payload map[string]interface{}, agent string) (*models.Job, error)
	Update(ctx context.Context, job *models.Job) error
	Delete(ctx context.Context, jobID string) error
	DeleteByRepoPRAndStacks(ctx context.Context, repo string, prNumber int, stackNames []string) (int, error)
}
