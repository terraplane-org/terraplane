package repository

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

type JobRepository interface {
	Create(ctx context.Context, job *models.Job) error
	Get(ctx context.Context, jobID string) (*models.Job, error)
	Update(ctx context.Context, job *models.Job) error
}
