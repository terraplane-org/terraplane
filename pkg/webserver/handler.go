package webserver

import (
	"net/http"

	"github.com/xyzjace/terraplane/pkg/log"
)

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func NewHandler(logger log.Logger) http.Handler {
	x := http.NewServeMux()
	x.HandleFunc("/health", healthCheckHandler)
	return x
}
