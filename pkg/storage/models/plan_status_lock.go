package models

import "time"

// PlanStatusLock is the row orchestrators lock while publishing the plan
// status for one repository and commit. The row is not the status. Holding
// it in a transaction stops a second orchestrator from posting an older
// snapshot after a newer one.
type PlanStatusLock struct {
	Repo      string `gorm:"primaryKey;size:255"`
	SHA       string `gorm:"primaryKey;size:64"`
	UpdatedAt time.Time
}
