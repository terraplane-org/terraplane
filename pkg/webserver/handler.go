package webserver

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type handler struct {
	logger      log.Logger
	scmProvider scm.Provider
	mux         *http.ServeMux
}

func NewHandler(logger log.Logger, scmProvider scm.Provider) http.Handler {
	h := &handler{
		logger:      logger,
		mux:         http.NewServeMux(),
		scmProvider: scmProvider,
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
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.Error("Failed to accept websocket connection", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to accept websocket connection"))
		return
	}
	defer c.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	var v any
	err = wsjson.Read(ctx, c, &v)
	if err != nil {
		h.logger.Error("Failed to read websocket message", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to read websocket message"))
		return
	}

	h.logger.Info("Received websocket message", "message", v)

	c.Close(websocket.StatusNormalClosure, "")
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
