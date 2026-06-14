package webserver

import (
	"net/http"

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

	return h
}

func (h *handler) scmWebhookHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("SCM webhook handler called")
	err := h.scmProvider.ParseWebhook(r)
	if err != nil {
		h.logger.Error("Failed to parse SCM webhook", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to parse SCM webhook"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Webhook parsed successfully"))
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
