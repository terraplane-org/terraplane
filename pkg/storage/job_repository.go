package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
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

func (r *jobRepository) UpsertPendingJob(ctx context.Context, repo string, prNumber int, stackName string, action string, payload map[string]interface{}, agent string) (*models.Job, error) {
	payloadJSON, err := marshalJobPayload(payload)
	if err != nil {
		return nil, err
	}

	var job models.Job
	err = r.db.pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"repo = ? AND pr_number = ? AND stack_name = ? AND action = ? AND status = ?",
				repo, prNumber, stackName, action, models.JobStatusPending,
			).
			Limit(1).
			Find(&job)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			job = models.Job{
				ID:        uuid.NewString(),
				Repo:      repo,
				PRNumber:  int32(prNumber),
				StackName: stackName,
				Dir:       stringFromPayload(payload, "dir"),
				CommitSHA: stringFromPayload(payload, "commit_sha"),
				AgentID:   agent,
				Action:    models.JobAction(action),
				Payload:   payloadJSON,
				Status:    models.JobStatusPending,
			}
			return tx.Create(&job).Error
		}

		job.AgentID = agent
		job.Payload = payloadJSON
		if dir := stringFromPayload(payload, "dir"); dir != "" {
			job.Dir = dir
		}
		if sha := stringFromPayload(payload, "commit_sha"); sha != "" {
			job.CommitSHA = sha
		}
		job.Output = ""
		job.ErrorMsg = ""
		job.LeaseExpiresAt = nil
		return tx.Save(&job).Error
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func marshalJobPayload(payload map[string]interface{}) (string, error) {
	if payload == nil {
		return "{}", nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal job payload: %w", err)
	}
	return string(b), nil
}

func stringFromPayload(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
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

func (r *jobRepository) Get(ctx context.Context, jobID string) (*models.Job, error) {
	var job models.Job
	err := r.db.pool.WithContext(ctx).First(&job, "id = ?", jobID).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) GetPendingJobsForAgents(ctx context.Context, agents []string) ([]*models.Job, error) {
	var jobs []*models.Job
	err := r.db.pool.WithContext(ctx).Where("agent_id IN ? AND status = ? ORDER BY created_at", agents, models.JobStatusPending).Find(&jobs).Error
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
