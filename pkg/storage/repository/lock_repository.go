package repository

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

type LockRepository interface {
	Get(ctx context.Context, repo, stackName, workspace string) (*models.ProjectLock, error)
	Create(ctx context.Context, lock *models.ProjectLock) error
	Delete(ctx context.Context, repo, stackName, workspace string) error
	DeleteByRepoAndStacks(ctx context.Context, repo string, stackNames []string) (int, error)
}
