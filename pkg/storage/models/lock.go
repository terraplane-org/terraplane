package models

import "time"

// ProjectLock coordinates exclusive access to a stack during apply.
// Uniqueness is enforced by idx_project_lock on (repo, stack_name, workspace).
type ProjectLock struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Repo      string `gorm:"size:255;not null;uniqueIndex:idx_project_lock"`
	StackName string `gorm:"size:255;not null;uniqueIndex:idx_project_lock"`
	Workspace string `gorm:"size:64;not null;default:default;uniqueIndex:idx_project_lock"`
	Dir       string `gorm:"size:512;not null"`

	PRNumber  int32  `gorm:"not null"`
	CommitSHA string `gorm:"size:64;not null"`
	LockedBy  string `gorm:"size:255;not null"`
}
