package webserver

import (
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
)

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
	_, err := h.scmProvider.ParseWebhook(r)
	if err != nil {
		h.logger.Error("Failed to parse SCM webhook", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to parse SCM webhook"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Webhook parsed successfully"))
}

func (h *handler) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.Error("Failed to accept websocket connection", "error", err)
		return
	}

	var agentID string
	if err := wsjson.Read(r.Context(), conn, &agentID); err != nil {
		h.logger.Error("Failed to read agent hello", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		// TODO: Send a message to the agent
		conn.Close(websocket.StatusInternalError, "failed to read agent hello")
		return
	}

	session := h.sessionFactory.New(agentID, conn)

	if err := h.sessionRegistry.Register(r.Context(), session); err != nil {
		h.logger.Error("Failed to register agent session", "agent_id", agentID, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		// TODO: Send a message to the agent
		conn.Close(websocket.StatusInternalError, "failed to register agent session")
		return
	}

	if err := session.Run(r.Context()); err != nil {
		h.logger.Error("Agent session ended with error", "agent_id", agentID, "error", err)
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
