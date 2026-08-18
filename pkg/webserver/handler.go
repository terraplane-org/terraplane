package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

const agentHelloTimeout = 10 * time.Second

type handler struct {
	logger          log.Logger
	scmProvider     scm.Provider
	scmPublisher    scm.Publisher
	mux             *http.ServeMux
	sessionRegistry agentsession.Registry
	sessionFactory  agentsession.Factory
	sharedAuthToken string
	jobService      services.JobService
}

func NewHandler(
	logger log.Logger,
	scmProvider scm.Provider,
	scmPublisher scm.Publisher,
	sessionRegistry agentsession.Registry,
	sessionFactory agentsession.Factory,
	jobService services.JobService,
	config *config.Config,
) http.Handler {
	h := &handler{
		logger:          logger,
		mux:             http.NewServeMux(),
		scmProvider:     scmProvider,
		scmPublisher:    scmPublisher,
		sessionRegistry: sessionRegistry,
		sessionFactory:  sessionFactory,
		sharedAuthToken: config.SharedAuthToken,
		jobService:      jobService,
	}

	h.mux.HandleFunc("GET /health", h.healthCheck)
	h.mux.HandleFunc("POST /scm/webhook", h.scmWebhookHandler)
	h.mux.Handle("GET /ws", h.requireBearer(http.HandlerFunc(h.websocketHandler)))
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

		if err := h.jobService.CreatePendingJobs(r.Context(), &webhook); err != nil {
			h.logger.Error(
				"Failed to create pending jobs from webhook",
				"repo", webhook.RepositorySlug,
				"pr", webhook.PRNumber,
				"error", err,
			)
		}

		// TODO: Is this really the appropriate place to react to the comment?
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

func (h *handler) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, wsproto.AcceptOptions())
	if err != nil {
		h.logger.Error("Failed to accept websocket connection", "error", err)
		return
	}
	wsproto.ConfigureConn(conn)

	helloCtx, cancel := context.WithTimeout(r.Context(), agentHelloTimeout)
	defer cancel()

	var hello terraplanev1.WebsocketEnvelope
	if err := wsproto.Read(helloCtx, conn, &hello); err != nil {
		h.logger.Error("Failed to read agent hello", "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "failed to read agent hello")
		return
	}

	agentID, err := agentIDFromHello(&hello)
	if err != nil {
		h.logger.Error("Invalid agent hello", "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	h.logger.Info("Received agent hello", "agent_id", agentID)

	session := h.sessionFactory.New(agentID, conn)

	if err := h.sessionRegistry.Register(r.Context(), session); err != nil {
		h.logger.Error("Failed to register agent session", "agent_id", agentID, "error", err)
		_ = conn.Close(websocket.StatusInternalError, "failed to register agent session")
		return
	}

	// TODO: Stop agent from being killed by various "errors" from the websocket connection like ACK receive
	if err := session.Run(r.Context()); err != nil {
		h.logger.Error("Agent session ended with error", "agent_id", agentID, "error", err)
	}
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
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agentJobClaimResponse{Command: *cmd})
}

func (h *handler) agentHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	var payload agentHeartbeatPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to unmarshal agent heartbeat request body", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to unmarshal agent heartbeat request body")
		return
	}

	err := h.jobService.RefreshAgentClaims(r.Context(), payload.AgentID)
	if err != nil {
		h.logger.Error("Failed to refresh agent claims", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to refresh agent claims")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) agentJobAckHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("Agent job ack handler called")
}

func (h *handler) agentJobResultHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("Agent job result handler called")
}

func agentIDFromHello(hello *terraplanev1.WebsocketEnvelope) (string, error) {
	helloMsg := hello.GetHello()
	if helloMsg == nil {
		return "", fmt.Errorf("expected hello payload")
	}
	if helloMsg.GetAgentId() == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	return helloMsg.GetAgentId(), nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, http.StatusOK, "OK")
}

func writeResponse(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
