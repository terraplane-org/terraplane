package models

import "time"

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
)

// Job tracks a single stack dispatch (plan/apply/unlock) over the agent websocket.
type Job struct {
	ID        string `gorm:"primaryKey;size:32"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Repo      string `gorm:"size:255;not null;index:idx_job_repo_pr"`
	PRNumber  int32  `gorm:"not null;index:idx_job_repo_pr"`
	StackName string `gorm:"size:255;not null"`
	Dir       string `gorm:"size:512;not null"`
	CommitSHA string `gorm:"size:64;not null"`

	Status   JobStatus `gorm:"size:32;not null"`
	Output   string    `gorm:"type:text"`
	ErrorMsg string    `gorm:"type:text"`
}
