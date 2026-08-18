package repository

import (
	"context"
	"errors"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

var (
	ErrLockExists  = errors.New("project lock already exists")
	ErrJobNotFound = errors.New("job not found")
)

//go:generate mockgen -source=lock_repository.go -destination=mock_repository/mock_lock_repository.go -package=mock_repository

type LockRepository interface {
	Get(ctx context.Context, repo, stackName, workspace string) (*models.ProjectLock, error)
	Create(ctx context.Context, lock *models.ProjectLock) error
	Delete(ctx context.Context, repo, stackName, workspace string) error
	DeleteByRepoAndStacks(ctx context.Context, repo string, stackNames []string) (int, error)
}
