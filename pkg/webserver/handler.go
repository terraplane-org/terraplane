package webserver

import (
	"encoding/json"
	"net/http"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
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
	h.mux.Handle("POST /agent/jobs/claim", h.requireBearer(http.HandlerFunc(h.agentJobClaimHandler)))
	h.mux.Handle("POST /agent/jobs/{id}/heartbeat", h.requireBearer(http.HandlerFunc(h.agentHeartbeatHandler)))
	h.mux.Handle("POST /agent/jobs/{id}/ack", h.requireBearer(http.HandlerFunc(h.agentJobAckHandler)))
	h.mux.Handle("POST /agent/jobs/{id}/result", h.requireBearer(http.HandlerFunc(h.agentJobResultHandler)))

	return h
}

func (h *handler) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.BearerTokenMatches(r.Header.Get("Authorization"), h.sharedAuthToken) {
			h.logger.Warn("Rejected request with invalid auth token", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
			writeResponse(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
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
		cmd := command.ParseWebhook(&webhook)
		if cmd.Kind == command.KindUnknown {
			h.logger.Debug(
				"Ignoring pull request comment that is not a terraplane command",
				"repo", webhook.RepositorySlug,
				"pr", webhook.PRNumber,
				"user", webhook.TriggeringUser,
				"comment", webhook.FullCommand,
			)
			continue
		}

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

func (h *handler) agentJobClaimHandler(w http.ResponseWriter, r *http.Request) {
	var payload agentJobClaimPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to unmarshal agent job claim request body", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to unmarshal agent job claim request body")
		return
	}

	cmd, err := h.jobService.ClaimPendingJob(r.Context(), payload.AgentID)
	if err != nil {
		h.logger.Error("Failed to claim pending job", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to claim pending job")
		return
	}
	if cmd == nil {
		writeNoContent(w)
		return
	}

	writeJSON(w, http.StatusOK, agentJobClaimResponse{Command: *cmd})
}

func (h *handler) agentHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	var payload agentHeartbeatPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to unmarshal agent heartbeat request body", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to unmarshal agent heartbeat request body")
		return
	}

	if err := h.jobService.RefreshAgentClaims(r.Context(), payload.AgentID); err != nil {
		h.logger.Error("Failed to refresh agent claims", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to refresh agent claims")
		return
	}

	writeNoContent(w)
}

func (h *handler) agentJobAckHandler(w http.ResponseWriter, r *http.Request) {
	var payload agentJobAckPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to unmarshal agent job ack request body", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to unmarshal agent job ack request body")
		return
	}

	jobID := r.PathValue("id")
	if err := h.jobService.AckJob(r.Context(), jobID, payload.AgentID); err != nil {
		h.logger.Error("Failed to ack job", "job_id", jobID, "agent_id", payload.AgentID, "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to ack job")
		return
	}

	writeNoContent(w)
}

func (h *handler) agentJobResultHandler(w http.ResponseWriter, r *http.Request) {
	var payload agentJobResultPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to unmarshal agent job result request body", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to unmarshal agent job result request body")
		return
	}

	jobID := r.PathValue("id")
	if err := h.jobService.CommitJobResult(r.Context(), jobID, payload.AgentID, payload.Result, payload.Output, payload.Error); err != nil {
		h.logger.Error("Failed to commit job result", "job_id", jobID, "agent_id", payload.AgentID, "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to commit job result")
		return
	}

	writeNoContent(w)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, http.StatusOK, "OK")
}

func writeResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
