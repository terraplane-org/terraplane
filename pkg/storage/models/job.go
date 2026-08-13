package models

import "time"

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
)

type JobAction string

const (
	JobActionPlan  JobAction = "plan"
	JobActionApply JobAction = "apply"
)

// Job tracks a single stack plan/apply for an agent to claim via HTTP polling.
type Job struct {
	ID        string `gorm:"primaryKey;size:36"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Repo      string `gorm:"size:255;not null;index:idx_job_repo_pr;index:idx_job_pending,priority:1"`
	PRNumber  int32  `gorm:"not null;index:idx_job_repo_pr"`
	StackName string `gorm:"size:255;not null;index:idx_job_pending,priority:2"`
	Dir       string `gorm:"size:512;not null"`
	CommitSHA string `gorm:"size:64;not null"`

	AgentID     string    `gorm:"size:255;not null;index:idx_job_agent_status"`
	Action      JobAction `gorm:"size:32;not null;index:idx_job_pending,priority:3"`
	PlanFlags   string    `gorm:"size:1024"`
	TriggerUser string    `gorm:"size:255"`

	Status         JobStatus  `gorm:"size:32;not null;default:'pending';index:idx_job_agent_status;index:idx_job_pending,priority:4"`
	LeaseExpiresAt *time.Time `gorm:"index:idx_job_lease"`
	Output         string     `gorm:"type:text"`
	ErrorMsg       string     `gorm:"type:text"`
}
