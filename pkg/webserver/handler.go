package webserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

const agentHelloTimeout = 10 * time.Second

type handler struct {
	logger          log.Logger
	scmProvider     scm.Provider
	mux             *http.ServeMux
	sessionRegistry agentsession.Registry
	sessionFactory  agentsession.Factory
}

func NewHandler(
	logger log.Logger,
	scmProvider scm.Provider,
	sessionRegistry agentsession.Registry,
	sessionFactory agentsession.Factory,
) http.Handler {
	h := &handler{
		logger:          logger,
		mux:             http.NewServeMux(),
		scmProvider:     scmProvider,
		sessionRegistry: sessionRegistry,
		sessionFactory:  sessionFactory,
	}

	h.mux.HandleFunc("GET /health", h.healthCheck)
	h.mux.HandleFunc("POST /scm/webhook", h.scmWebhookHandler)
	h.mux.HandleFunc("GET /ws", h.websocketHandler)

	return h
}

func (h *handler) scmWebhookHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("SCM webhook handler called")
	webhook, err := h.scmProvider.ParseWebhook(r)
	if err != nil {
		h.logger.Error("Failed to parse SCM webhook", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to parse SCM webhook")
		return
	}
	if webhook == nil {
		h.logger.Debug("No actionable SCM webhook event found")
		writeResponse(w, http.StatusOK, "No actionable event found")
		return
	}

	command := command.ParseWebhook(&webhook[0])

	h.handleScmWebhook(command)

	writeResponse(w, http.StatusOK, "Webhook parsed successfully")
}

func (h *handler) handleScmWebhook(cmd command.Command) {
	switch cmd.Kind {
	case command.KindPlan:
		h.logger.Info("Parsed plan command", "repo", cmd.Plan.Repo, "pr", cmd.Plan.PRNumber, "user", cmd.Plan.TriggerUser, "stacks", cmd.Plan.Stacks, "commit", cmd.Plan.CommitSHA)
	case command.KindApply:
		h.logger.Info("Parsed apply command", "repo", cmd.Apply.Repo, "pr", cmd.Apply.PRNumber, "user", cmd.Apply.TriggerUser)
	case command.KindUnlock:
		h.logger.Info("Parsed unlock command", "repo", cmd.Unlock.Repo, "pr", cmd.Unlock.PRNumber, "user", cmd.Unlock.TriggerUser)
	case command.KindUnknown:
		h.logger.Debug("Ignoring unrecognized command")
	default:
		h.logger.Warn("Unhandled command kind", "kind", cmd.Kind)
	}
}

func (h *handler) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.Error("Failed to accept websocket connection", "error", err)
		return
	}

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

	if err := session.Run(r.Context()); err != nil {
		h.logger.Error("Agent session ended with error", "agent_id", agentID, "error", err)
	}
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
