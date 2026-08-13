package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func (r *jobRepository) GetPending(ctx context.Context, repo, stackName string, action models.JobAction) (*models.Job, error) {
	var job models.Job
	err := r.db.pool.WithContext(ctx).
		Where("repo = ? AND stack_name = ? AND action = ? AND status = ?",
			repo, stackName, action, models.JobStatusPending).
		First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) UpsertPending(ctx context.Context, job *models.Job) (*models.Job, error) {
	var existing models.Job
	err := r.db.pool.WithContext(ctx).
		Where("repo = ? AND stack_name = ? AND action = ? AND status = ?",
			job.Repo, job.StackName, job.Action, models.JobStatusPending).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := r.Create(ctx, job); err != nil {
			return nil, err
		}
		return job, nil
	}
	if err != nil {
		return nil, err
	}

	existing.PRNumber = job.PRNumber
	existing.Dir = job.Dir
	existing.CommitSHA = job.CommitSHA
	existing.AgentID = job.AgentID
	existing.PlanFlags = job.PlanFlags
	existing.TriggerUser = job.TriggerUser
	existing.Output = ""
	existing.ErrorMsg = ""
	existing.LeaseExpiresAt = nil
	if err := r.Update(ctx, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *jobRepository) ClaimNext(ctx context.Context, agentID string, lease time.Duration) (*models.Job, error) {
	var claimed *models.Job
	err := r.db.pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job models.Job
		// Oldest pending job for this agent whose stack is not already running.
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("agent_id = ? AND status = ?", agentID, models.JobStatusPending).
			Where(`NOT EXISTS (
				SELECT 1 FROM jobs AS running
				WHERE running.repo = jobs.repo
				  AND running.stack_name = jobs.stack_name
				  AND running.status = ?
			)`, models.JobStatusRunning).
			Order("created_at ASC").
			First(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}

		expires := time.Now().UTC().Add(lease)
		job.Status = models.JobStatusRunning
		job.LeaseExpiresAt = &expires
		if err := tx.Save(&job).Error; err != nil {
			return fmt.Errorf("claim job %s: %w", job.ID, err)
		}
		claimed = &job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *jobRepository) FailExpiredLeases(ctx context.Context, now time.Time) ([]models.Job, error) {
	var expired []models.Job
	err := r.db.pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?", models.JobStatusRunning, now).
			Find(&expired).Error; err != nil {
			return err
		}
		for i := range expired {
			expired[i].Status = models.JobStatusFailed
			expired[i].ErrorMsg = "job lease expired before the agent reported a result"
			expired[i].LeaseExpiresAt = nil
			if err := tx.Save(&expired[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return expired, nil
}
