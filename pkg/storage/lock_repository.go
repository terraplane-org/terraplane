package storage

import (
	"context"
	"errors"

	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"gorm.io/gorm"
)

type lockRepository struct {
	db *DB
}

func NewLockRepository(db *DB) repository.LockRepository {
	return &lockRepository{db: db}
}

func (r *lockRepository) Get(ctx context.Context, repo, stackName, workspace string) (*models.ProjectLock, error) {
	var lock models.ProjectLock
	err := r.db.pool.WithContext(ctx).
		Where("repo = ? AND stack_name = ? AND workspace = ?", repo, stackName, workspace).
		First(&lock).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lock, nil
}

func (r *lockRepository) Create(ctx context.Context, lock *models.ProjectLock) error {
	err := r.db.pool.WithContext(ctx).Create(lock).Error
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return repository.ErrLockExists
		}
		return err
	}
	return nil
}

func (r *lockRepository) Delete(ctx context.Context, repo, stackName, workspace string) error {
	return r.db.pool.WithContext(ctx).
		Where("repo = ? AND stack_name = ? AND workspace = ?", repo, stackName, workspace).
		Delete(&models.ProjectLock{}).Error
}

func (r *lockRepository) DeleteByRepoAndStacks(ctx context.Context, repo string, stackNames []string) (int, error) {
	if len(stackNames) == 0 {
		return 0, nil
	}

	result := r.db.pool.WithContext(ctx).
		Where("repo = ? AND stack_name IN ?", repo, stackNames).
		Delete(&models.ProjectLock{})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}
