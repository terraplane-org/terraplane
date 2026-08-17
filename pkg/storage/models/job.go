package models

import "time"

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusClaimed   JobStatus = "claimed"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
)

type JobAction string

const (
	JobActionPlan  JobAction = "plan"
	JobActionApply JobAction = "apply"
)

// Job tracks a unit of agent work (plan/apply) for durable enqueue and claim.
//
// Command-specific details that may grow over time (plan flags, etc.) live in
// Payload as JSON so the table schema stays stable.
type Job struct {
	ID        string `gorm:"primaryKey;size:36"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Repo      string `gorm:"size:255;not null;index:idx_job_repo_pr;index:idx_job_pending,priority:1"`
	PRNumber  int32  `gorm:"not null;index:idx_job_repo_pr"`
	StackName string `gorm:"size:255;not null;index:idx_job_pending,priority:2"`
	Dir       string `gorm:"size:512;not null"`
	CommitSHA string `gorm:"size:64;not null"`

	AgentID string    `gorm:"size:255;not null;index:idx_job_agent_status"`
	Action  JobAction `gorm:"size:32;not null;index:idx_job_pending,priority:3"`
	// Payload is JSON (e.g. {"plan_flags":"...","trigger_user":"..."}).
	Payload string `gorm:"type:jsonb;not null;default:'{}'"`

	Status         JobStatus  `gorm:"size:32;not null;default:'pending';index:idx_job_agent_status;index:idx_job_pending,priority:4"`
	LeaseExpiresAt *time.Time `gorm:"index:idx_job_lease"`
	Output         string     `gorm:"type:text"`
	ErrorMsg       string     `gorm:"type:text"`
}
