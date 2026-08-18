package webserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/agentapi"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
)

type handler struct {
	logger          log.Logger
	scmProvider     scm.Provider
	scmPublisher    scm.Publisher
	mux             *http.ServeMux
	sharedAuthToken string
	jobService      services.JobService
}

func NewHandler(
	logger log.Logger,
	scmProvider scm.Provider,
	scmPublisher scm.Publisher,
	jobService services.JobService,
	config *config.Config,
) http.Handler {
	h := &handler{
		logger:          logger,
		mux:             http.NewServeMux(),
		scmProvider:     scmProvider,
		scmPublisher:    scmPublisher,
		sharedAuthToken: config.SharedAuthToken,
		jobService:      jobService,
	}

	h.mux.HandleFunc("GET /health", h.healthCheck)
	h.mux.HandleFunc("POST /scm/webhook", h.scmWebhookHandler)
	h.mux.HandleFunc("POST /agent/jobs/poll", h.agentPollHandler)
	h.mux.HandleFunc("POST /agent/jobs/{id}/heartbeat", h.agentHeartbeatHandler)
	h.mux.HandleFunc("POST /agent/jobs/{id}/ack", h.agentAckHandler)
	h.mux.HandleFunc("POST /agent/jobs/{id}/result", h.agentResultHandler)

	return h
}

func (h *handler) scmWebhookHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("SCM webhook handler called")

	webhooks, err := h.scmProvider.ParseWebhook(r)
	if err != nil {
		h.logger.Error("Failed to parse SCM webhook", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to parse SCM webhook")
		return
	}
	if len(webhooks) == 0 {
		h.logger.Debug("No actionable SCM webhook events found")
		writeResponse(w, http.StatusOK, "No actionable event found")
		return
	}

	for _, webhook := range webhooks {
		if err := h.jobService.CreatePendingJobs(r.Context(), &webhook); err != nil {
			h.logger.Error(
				"Failed to create pending jobs from webhook",
				"repo", webhook.RepositorySlug,
				"pr", webhook.PRNumber,
				"error", err,
			)
		}

		if err := h.scmPublisher.AcknowledgeComment(
			r.Context(),
			webhook.RepositorySlug,
			webhook.PRNumber,
			webhook.CommentID,
		); err != nil {
			h.logger.Error(
				"Failed to acknowledge pull request comment",
				"repo", webhook.RepositorySlug,
				"pr", webhook.PRNumber,
				"comment_id", webhook.CommentID,
				"error", err,
			)
		}
	}

	writeResponse(w, http.StatusOK, "Webhook parsed successfully")
}

func (h *handler) agentPollHandler(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.agentAuth(w, r)
	if !ok {
		return
	}

	job, err := h.jobService.PollJob(r.Context(), agentID)
	if err != nil {
		h.logger.Error("Failed to poll job", "agent_id", agentID, "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to poll job")
		return
	}
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (h *handler) agentHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.agentAuth(w, r)
	if !ok {
		return
	}
	if err := h.jobService.Heartbeat(r.Context(), agentID, r.PathValue("id")); err != nil {
		h.writeAgentJobError(w, "heartbeat", agentID, r.PathValue("id"), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) agentAckHandler(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.agentAuth(w, r)
	if !ok {
		return
	}
	if err := h.jobService.AckJob(r.Context(), agentID, r.PathValue("id")); err != nil {
		h.writeAgentJobError(w, "ack", agentID, r.PathValue("id"), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) agentResultHandler(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.agentAuth(w, r)
	if !ok {
		return
	}

	var result agentapi.Result
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&result); err != nil {
		writeResponse(w, http.StatusBadRequest, "Invalid result payload")
		return
	}

	if err := h.jobService.RecordResult(r.Context(), agentID, r.PathValue("id"), result); err != nil {
		h.writeAgentJobError(w, "result", agentID, r.PathValue("id"), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) agentAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !auth.BearerTokenMatches(r.Header.Get("Authorization"), h.sharedAuthToken) {
		writeResponse(w, http.StatusUnauthorized, "Unauthorized")
		return "", false
	}
	agentID := r.Header.Get(agentapi.AgentIDHeader)
	if agentID == "" {
		writeResponse(w, http.StatusBadRequest, "X-Agent-ID is required")
		return "", false
	}
	return agentID, true
}

func (h *handler) writeAgentJobError(w http.ResponseWriter, op, agentID, jobID string, err error) {
	if errors.Is(err, repository.ErrJobNotFound) {
		writeResponse(w, http.StatusNotFound, "Job not found")
		return
	}
	h.logger.Error("Agent job request failed", "op", op, "agent_id", agentID, "job_id", jobID, "error", err)
	writeResponse(w, http.StatusInternalServerError, "Failed to update job")
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, http.StatusOK, "OK")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeResponse(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
