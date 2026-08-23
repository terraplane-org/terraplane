package services

import (
	"fmt"

	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func latestPlanJobsForSHA(jobs []*models.Job, sha string) []*models.Job {
	latest := make(map[string]*models.Job)
	for _, job := range jobs {
		if job == nil || job.Action != models.JobActionPlan || job.CommitSHA != sha {
			continue
		}
		prev, ok := latest[job.StackName]
		if !ok || job.UpdatedAt.After(prev.UpdatedAt) || (job.UpdatedAt.Equal(prev.UpdatedAt) && job.ID > prev.ID) {
			latest[job.StackName] = job
		}
	}
	out := make([]*models.Job, 0, len(latest))
	for _, job := range latest {
		out = append(out, job)
	}
	return out
}

func planRollup(jobs []*models.Job) (scm.CheckState, string) {
	if len(jobs) == 0 {
		return scm.CheckPending, "no plans"
	}

	var pending, failed, succeeded int
	for _, job := range jobs {
		switch job.Status {
		case models.JobStatusSucceeded:
			succeeded++
		case models.JobStatusFailed:
			failed++
		default:
			pending++
		}
	}

	total := len(jobs)
	done := succeeded + failed
	switch {
	case pending > 0:
		return scm.CheckPending, fmt.Sprintf("%d/%d plans complete", done, total)
	case failed > 0:
		return scm.CheckFailure, fmt.Sprintf("%d/%d plans failed", failed, total)
	default:
		return scm.CheckSuccess, fmt.Sprintf("%d/%d plans succeeded", succeeded, total)
	}
}

func planCheckDescription(job *models.Job) string {
	switch job.Status {
	case models.JobStatusSucceeded:
		return "plan succeeded"
	case models.JobStatusFailed:
		return "plan failed"
	default:
		return "planning"
	}
}

func planCheckState(job *models.Job) scm.CheckState {
	switch job.Status {
	case models.JobStatusSucceeded:
		return scm.CheckSuccess
	case models.JobStatusFailed:
		return scm.CheckFailure
	default:
		return scm.CheckPending
	}
}
