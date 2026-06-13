package webserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
)

type Server interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type server struct {
	logger log.Logger
	server *http.Server
}

func (o *server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		o.logger.Info("Web server started", "addr", o.server.Addr)
		errCh <- o.server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
}

func (o *server) Shutdown(ctx context.Context) error {
	o.logger.Debug("Web server shutdown")
	return o.server.Shutdown(ctx)
}

func NewServer(config *config.Config, logger log.Logger, handler http.Handler) Server {
	s := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", config.OrchestratorListenAddress, config.OrchestratorListenPort),
		Handler: handler,
	}
	return &server{
		logger: logger,
		server: s,
	}
}
