package storage

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
)

type jobRepository struct {
	db *DB
}

func NewJobRepository(db *DB) repository.JobRepository {
	return &jobRepository{db: db}
}

func (r *jobRepository) Create(ctx context.Context, job *models.Job) error {
	return r.db.pool.WithContext(ctx).Create(job).Error
}

func (r *jobRepository) Get(ctx context.Context, jobID string) (*models.Job, error) {
	var job models.Job
	err := r.db.pool.WithContext(ctx).First(&job, "id = ?", jobID).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) FindByRepoPRAndStack(ctx context.Context, repo string, prNumber int, stackName string) ([]*models.Job, error) {
	var jobs []*models.Job
	err := r.db.pool.WithContext(ctx).Where("repo = ? AND pr_number = ? AND stack_name = ?", repo, prNumber, stackName).Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *jobRepository) Update(ctx context.Context, job *models.Job) error {
	return r.db.pool.WithContext(ctx).Save(job).Error
}

func (r *jobRepository) Delete(ctx context.Context, jobID string) error {
	return r.db.pool.WithContext(ctx).Delete(&models.Job{}, "id = ?", jobID).Error
}

func (r *jobRepository) DeleteByRepoPRAndStacks(ctx context.Context, repo string, prNumber int, stackNames []string) (int, error) {
	if len(stackNames) == 0 {
		return 0, nil
	}

	result := r.db.pool.WithContext(ctx).
		Where("repo = ? AND pr_number = ? AND stack_name IN ?", repo, prNumber, stackNames).
		Delete(&models.Job{})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}
