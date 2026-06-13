package webserver

import (
	"net/http"

	"github.com/xyzjace/terraplane/pkg/log"
)

type handler struct {
	logger log.Logger
	mux    *http.ServeMux
}

func NewHandler(logger log.Logger) http.Handler {
	h := &handler{
		logger: logger,
		mux:    http.NewServeMux(),
	}

	h.mux.HandleFunc("GET /health", h.healthCheck)

	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
