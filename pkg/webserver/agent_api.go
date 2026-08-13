package webserver

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

type claimedJobResponse struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Repo      string `json:"repo"`
	PRNumber  int32  `json:"pr_number"`
	StackName string `json:"stack_name"`
	Dir       string `json:"dir"`
	CommitSHA string `json:"commit_sha"`
	PlanFlags string `json:"plan_flags,omitempty"`
}

type jobResultRequest struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

func claimedJobFromModel(job *models.Job) claimedJobResponse {
	return claimedJobResponse{
		ID:        job.ID,
		Action:    string(job.Action),
		Repo:      job.Repo,
		PRNumber:  job.PRNumber,
		StackName: job.StackName,
		Dir:       job.Dir,
		CommitSHA: job.CommitSHA,
		PlanFlags: job.PlanFlags,
	}
}

func (h *handler) agentClaimHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAgent(r) {
		writeResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	agentID := r.PathValue("agentID")
	wait := 30 * time.Second
	if raw := r.URL.Query().Get("wait"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			writeResponse(w, http.StatusBadRequest, "invalid wait duration")
			return
		}
		wait = d
	}

	job, err := h.claimService.Claim(r.Context(), agentID, wait)
	if err != nil {
		h.logger.Error("Failed to claim job", "agent_id", agentID, "error", err)
		writeResponse(w, http.StatusInternalServerError, "failed to claim job")
		return
	}
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(claimedJobFromModel(job))
}

func (h *handler) agentResultHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAgent(r) {
		writeResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	agentID := r.PathValue("agentID")
	jobID := r.PathValue("jobID")

	var body jobResultRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeResponse(w, http.StatusBadRequest, "invalid result body")
		return
	}

	if err := h.resultService.Complete(r.Context(), agentID, jobID, body.Success, body.Output, body.Error); err != nil {
		h.logger.Error("Failed to complete job", "agent_id", agentID, "job_id", jobID, "error", err)
		writeResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) authorizeAgent(r *http.Request) bool {
	return authBearerMatches(r, h.sharedAuthToken)
}
