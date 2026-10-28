package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func TestLatestPlanJobsForSHA(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	jobs := []*models.Job{
		{ID: "1", StackName: "a", CommitSHA: "sha1", Action: models.JobActionPlan, UpdatedAt: older},
		{ID: "2", StackName: "a", CommitSHA: "sha1", Action: models.JobActionPlan, UpdatedAt: newer},
		{ID: "3", StackName: "b", CommitSHA: "sha1", Action: models.JobActionApply, UpdatedAt: newer},
		{ID: "4", StackName: "c", CommitSHA: "other", Action: models.JobActionPlan, UpdatedAt: newer},
	}
	got := latestPlanJobsForSHA(jobs, "sha1")
	require.Len(t, got, 1)
	require.Equal(t, "2", got[0].ID)
}

func TestPlanRollup(t *testing.T) {
	state, desc := planRollup(nil)
	require.Equal(t, scm.CheckPending, state)
	require.Equal(t, "no plans", desc)

	state, desc = planRollup([]*models.Job{
		{Status: models.JobStatusPending},
		{Status: models.JobStatusSucceeded},
	})
	require.Equal(t, scm.CheckPending, state)
	require.Equal(t, "1/2 plans complete", desc)

	state, desc = planRollup([]*models.Job{
		{Status: models.JobStatusSucceeded},
		{Status: models.JobStatusFailed},
	})
	require.Equal(t, scm.CheckFailure, state)
	require.Equal(t, "1/2 plans failed", desc)

	state, desc = planRollup([]*models.Job{
		{Status: models.JobStatusSucceeded},
		{Status: models.JobStatusSucceeded},
	})
	require.Equal(t, scm.CheckSuccess, state)
	require.Equal(t, "2/2 plans succeeded", desc)
}
