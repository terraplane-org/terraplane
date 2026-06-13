package webserver

import (
	"context"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
)

type Server interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type server struct {
	logger log.Logger
}

func (o *server) Start(ctx context.Context) error {
	o.logger.Info("Web server started")

	<-ctx.Done()
	return nil
}

func (o *server) Shutdown(ctx context.Context) error {
	o.logger.Debug("Web server shutdown")
	return nil
}

func NewServer(config *config.Config, logger log.Logger) Server {
	return &server{
		logger: logger,
	}
}
