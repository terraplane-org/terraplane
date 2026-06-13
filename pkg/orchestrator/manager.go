package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/webserver"
	"golang.org/x/sync/errgroup"
)

type Manager interface {
	Start(ctx context.Context) error
}

type manager struct {
	serverShutdownTimer time.Duration
	logger              log.Logger
	server              webserver.Server
}

func (o *manager) Start(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		o.logger.Debug("Starting web server")

		err := o.server.Start(ctx)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	})

	group.Go(func() error {
		<-ctx.Done()

		_, cancel := context.WithTimeout(
			context.Background(),
			o.serverShutdownTimer,
		)
		defer cancel()

		return o.server.Shutdown()
	})

	if err := group.Wait(); err != nil {
		o.logger.Error(fmt.Sprintf("something went wrong, stopping orchestrator processes: %v", err))
		return err
	}

	return nil
}

func NewManager(config *config.Config, logger log.Logger, server webserver.Server) Manager {
	return &manager{
		serverShutdownTimer: config.ServerShutdownTimer,
		logger:              logger,
		server:              server,
	}
}
