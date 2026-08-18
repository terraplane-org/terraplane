package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testJobRepo(t *testing.T) repository.JobRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/jobs.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Job{}))
	return NewJobRepository(&DB{pool: db})
}

func ptrTime(t time.Time) *time.Time { return &t }

func createJob(t *testing.T, repo repository.JobRepository, job *models.Job) *models.Job {
	t.Helper()
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.Repo == "" {
		job.Repo = "acme/infra"
	}
	if job.StackName == "" {
		job.StackName = "a"
	}
	if job.Dir == "" {
		job.Dir = "stacks/a"
	}
	if job.CommitSHA == "" {
		job.CommitSHA = "abc123"
	}
	if job.AgentID == "" {
		job.AgentID = "agent-a"
	}
	if job.Action == "" {
		job.Action = models.JobActionPlan
	}
	if job.Payload == "" {
		job.Payload = "{}"
	}
	if job.Status == "" {
		job.Status = models.JobStatusPending
	}
	require.NoError(t, repo.Create(context.Background(), job))
	return job
}

func getJob(t *testing.T, repo repository.JobRepository, id string) *models.Job {
	t.Helper()
	job, err := repo.Get(context.Background(), id)
	require.NoError(t, err)
	return job
}

func TestRefreshAgentClaimsExtendsClaimedAndRunningLeases(t *testing.T) {
	repo := testJobRepo(t)
	oldLease := time.Now().Add(-time.Minute)
	claimed := createJob(t, repo, &models.Job{Status: models.JobStatusClaimed, LeaseExpiresAt: ptrTime(oldLease)})
	running := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "b", Dir: "stacks/b",
		Status: models.JobStatusRunning, LeaseExpiresAt: ptrTime(oldLease),
	})
	pending := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "c", Dir: "stacks/c",
		Status: models.JobStatusPending, LeaseExpiresAt: ptrTime(oldLease),
	})
	otherAgent := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "d", Dir: "stacks/d", AgentID: "agent-b",
		Status: models.JobStatusClaimed, LeaseExpiresAt: ptrTime(oldLease),
	})
	succeeded := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "e", Dir: "stacks/e",
		Status: models.JobStatusSucceeded, LeaseExpiresAt: ptrTime(oldLease),
	})

	nextLease := time.Now().Add(2 * time.Minute).UTC().Truncate(time.Second)
	require.NoError(t, repo.RefreshAgentClaims(context.Background(), "agent-a", &nextLease))

	require.Equal(t, nextLease, getJob(t, repo, claimed.ID).LeaseExpiresAt.UTC())
	require.Equal(t, nextLease, getJob(t, repo, running.ID).LeaseExpiresAt.UTC())
	require.Equal(t, oldLease.UTC().Truncate(time.Second), getJob(t, repo, pending.ID).LeaseExpiresAt.UTC().Truncate(time.Second))
	require.Equal(t, oldLease.UTC().Truncate(time.Second), getJob(t, repo, otherAgent.ID).LeaseExpiresAt.UTC().Truncate(time.Second))
	require.Equal(t, oldLease.UTC().Truncate(time.Second), getJob(t, repo, succeeded.ID).LeaseExpiresAt.UTC().Truncate(time.Second))
}

func TestReapExpiredClaimsReturnsClaimedJobsToPending(t *testing.T) {
	repo := testJobRepo(t)
	now := time.Now().UTC().Truncate(time.Second)
	expired := createJob(t, repo, &models.Job{
		Status: models.JobStatusClaimed, LeaseExpiresAt: ptrTime(now.Add(-time.Second)),
	})
	fresh := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "b", Dir: "stacks/b",
		Status: models.JobStatusClaimed, LeaseExpiresAt: ptrTime(now.Add(time.Minute)),
	})
	noLease := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "c", Dir: "stacks/c",
		Status: models.JobStatusClaimed,
	})
	runningExpired := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "d", Dir: "stacks/d",
		Status: models.JobStatusRunning, LeaseExpiresAt: ptrTime(now.Add(-time.Second)),
	})

	n, err := repo.ReapExpiredClaims(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got := getJob(t, repo, expired.ID)
	require.Equal(t, models.JobStatusPending, got.Status)
	require.Nil(t, got.LeaseExpiresAt)

	got = getJob(t, repo, fresh.ID)
	require.Equal(t, models.JobStatusClaimed, got.Status)
	require.NotNil(t, got.LeaseExpiresAt)

	got = getJob(t, repo, noLease.ID)
	require.Equal(t, models.JobStatusClaimed, got.Status)
	require.Nil(t, got.LeaseExpiresAt)

	got = getJob(t, repo, runningExpired.ID)
	require.Equal(t, models.JobStatusRunning, got.Status)
	require.NotNil(t, got.LeaseExpiresAt)
}

func TestClaimPendingJobForAgentSetsLease(t *testing.T) {
	repo := testJobRepo(t)
	createJob(t, repo, &models.Job{Status: models.JobStatusPending, Action: models.JobActionPlan})
	lease := time.Now().Add(time.Minute).UTC().Truncate(time.Second)

	job, err := repo.ClaimPendingJobForAgent(context.Background(), "agent-a", models.JobStatusClaimed, &lease)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, models.JobStatusClaimed, job.Status)
	require.Equal(t, lease, job.LeaseExpiresAt.UTC())

	got := getJob(t, repo, job.ID)
	require.Equal(t, models.JobStatusClaimed, got.Status)
	require.Equal(t, lease, got.LeaseExpiresAt.UTC())
}

func TestClaimPendingJobForAgentEmptyIDClaimsUnlockOnly(t *testing.T) {
	repo := testJobRepo(t)
	plan := createJob(t, repo, &models.Job{Status: models.JobStatusPending, Action: models.JobActionPlan})
	unlock := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "b", Dir: "stacks/b", AgentID: "agent-b",
		Status: models.JobStatusPending, Action: models.JobActionUnlock,
	})
	lease := time.Now().Add(time.Minute)

	job, err := repo.ClaimPendingJobForAgent(context.Background(), "", models.JobStatusClaimed, &lease)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, unlock.ID, job.ID)
	require.Equal(t, models.JobActionUnlock, job.Action)

	require.Equal(t, models.JobStatusPending, getJob(t, repo, plan.ID).Status)
}

func TestClaimPendingJobForAgentSkipsBusyStack(t *testing.T) {
	repo := testJobRepo(t)
	createJob(t, repo, &models.Job{
		Status: models.JobStatusClaimed, Action: models.JobActionPlan, StackName: "a",
	})
	createJob(t, repo, &models.Job{
		ID: uuid.NewString(), PRNumber: 43, Status: models.JobStatusPending, Action: models.JobActionPlan, StackName: "a",
	})
	other := createJob(t, repo, &models.Job{
		ID: uuid.NewString(), StackName: "b", Dir: "stacks/b",
		Status: models.JobStatusPending, Action: models.JobActionPlan,
	})
	lease := time.Now().Add(time.Minute)

	job, err := repo.ClaimPendingJobForAgent(context.Background(), "agent-a", models.JobStatusClaimed, &lease)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, other.ID, job.ID)
}

func TestClaimPendingJobForAgentReturnsNilWhenNone(t *testing.T) {
	repo := testJobRepo(t)
	job, err := repo.ClaimPendingJobForAgent(context.Background(), "agent-a", models.JobStatusClaimed, ptrTime(time.Now()))
	require.NoError(t, err)
	require.Nil(t, job)
}

func TestReleaseClaimedJobClearsLease(t *testing.T) {
	repo := testJobRepo(t)
	claimed := createJob(t, repo, &models.Job{
		Status: models.JobStatusClaimed, LeaseExpiresAt: ptrTime(time.Now().Add(time.Minute)),
	})
	require.NoError(t, repo.ReleaseClaimedJob(context.Background(), claimed.ID))

	got := getJob(t, repo, claimed.ID)
	require.Equal(t, models.JobStatusPending, got.Status)
	require.Nil(t, got.LeaseExpiresAt)
}

func TestFailClaimedJobClearsLease(t *testing.T) {
	repo := testJobRepo(t)
	claimed := createJob(t, repo, &models.Job{
		Status: models.JobStatusClaimed, LeaseExpiresAt: ptrTime(time.Now().Add(time.Minute)),
	})
	require.NoError(t, repo.FailClaimedJob(context.Background(), claimed.ID, "boom"))

	got := getJob(t, repo, claimed.ID)
	require.Equal(t, models.JobStatusFailed, got.Status)
	require.Equal(t, "boom", got.ErrorMsg)
	require.Nil(t, got.LeaseExpiresAt)
}

func TestUpsertPendingJobCreatesThenUpdates(t *testing.T) {
	repo := testJobRepo(t)
	created, err := repo.UpsertPendingJob(
		context.Background(), "acme/infra", 42, "a", "plan",
		map[string]interface{}{"dir": "stacks/a", "commit_sha": "aaa"},
		"agent-a",
	)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusPending, created.Status)
	require.Equal(t, "agent-a", created.AgentID)

	updated, err := repo.UpsertPendingJob(
		context.Background(), "acme/infra", 42, "a", "plan",
		map[string]interface{}{"dir": "stacks/a2", "commit_sha": "bbb", "plan_flags": "-target=x"},
		"agent-z",
	)
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, "agent-z", updated.AgentID)
	require.Equal(t, "stacks/a2", updated.Dir)
	require.Equal(t, "bbb", updated.CommitSHA)
	require.Contains(t, updated.Payload, "-target=x")
	require.Nil(t, updated.LeaseExpiresAt)
}

func TestUpsertPendingJobLeavesMissingDirAndSHAUnchanged(t *testing.T) {
	repo := testJobRepo(t)
	created, err := repo.UpsertPendingJob(
		context.Background(), "acme/infra", 42, "a", "plan",
		map[string]interface{}{"dir": "stacks/a", "commit_sha": "aaa"},
		"agent-a",
	)
	require.NoError(t, err)

	updated, err := repo.UpsertPendingJob(
		context.Background(), "acme/infra", 42, "a", "plan",
		map[string]interface{}{"plan_flags": "-refresh"},
		"agent-a",
	)
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, "stacks/a", updated.Dir)
	require.Equal(t, "aaa", updated.CommitSHA)
}

func TestUpsertPendingJobDoesNotClobberClaimed(t *testing.T) {
	repo := testJobRepo(t)
	claimed := createJob(t, repo, &models.Job{Status: models.JobStatusClaimed, PRNumber: 42})
	created, err := repo.UpsertPendingJob(
		context.Background(), "acme/infra", 42, "a", "plan",
		map[string]interface{}{"dir": "stacks/a", "commit_sha": "abc123"},
		"agent-a",
	)
	require.NoError(t, err)
	require.NotEqual(t, claimed.ID, created.ID)
	require.Equal(t, models.JobStatusClaimed, getJob(t, repo, claimed.ID).Status)
}

func TestUpsertPendingJobInvalidPayload(t *testing.T) {
	repo := testJobRepo(t)
	_, err := repo.UpsertPendingJob(
		context.Background(), "acme/infra", 42, "a", "plan",
		map[string]interface{}{"bad": make(chan int)},
		"agent-a",
	)
	require.Error(t, err)
}

func TestGetMissingJob(t *testing.T) {
	repo := testJobRepo(t)
	_, err := repo.Get(context.Background(), "missing")
	require.Error(t, err)
}

func TestClaimPendingJobForAgentIgnoresOtherAgents(t *testing.T) {
	repo := testJobRepo(t)
	createJob(t, repo, &models.Job{AgentID: "agent-b", Status: models.JobStatusPending})
	job, err := repo.ClaimPendingJobForAgent(context.Background(), "agent-a", models.JobStatusClaimed, ptrTime(time.Now()))
	require.NoError(t, err)
	require.Nil(t, job)
}

func TestDeleteByRepoPRAndStacksNoNames(t *testing.T) {
	repo := testJobRepo(t)
	n, err := repo.DeleteByRepoPRAndStacks(context.Background(), "acme/infra", 42, nil)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestUpdateAndDelete(t *testing.T) {
	repo := testJobRepo(t)
	job := createJob(t, repo, &models.Job{})
	job.Output = "ok"
	require.NoError(t, repo.Update(context.Background(), job))
	require.Equal(t, "ok", getJob(t, repo, job.ID).Output)

	require.NoError(t, repo.Delete(context.Background(), job.ID))
	_, err := repo.Get(context.Background(), job.ID)
	require.Error(t, err)
}

func TestDeleteByRepoPRAndStacks(t *testing.T) {
	repo := testJobRepo(t)
	keep := createJob(t, repo, &models.Job{StackName: "keep", Dir: "stacks/keep"})
	drop := createJob(t, repo, &models.Job{StackName: "drop", Dir: "stacks/drop", PRNumber: 42})
	n, err := repo.DeleteByRepoPRAndStacks(context.Background(), "acme/infra", 42, []string{"drop"})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, keep.ID, getJob(t, repo, keep.ID).ID)
	_, err = repo.Get(context.Background(), drop.ID)
	require.Error(t, err)
}
