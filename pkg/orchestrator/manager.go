package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/storage"
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
	db                  *storage.DB
	sharedAuthToken     string
}

func (o *manager) Start(ctx context.Context) error {
	if o.sharedAuthToken == "" {
		return fmt.Errorf("SHARED_AUTH_TOKEN is not configured")
	}

	if err := o.db.RequireCurrentSchema(ctx); err != nil {
		return err
	}

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

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			o.serverShutdownTimer,
		)
		defer cancel()

		return o.server.Shutdown(shutdownCtx)
	})

	if err := group.Wait(); err != nil {
		o.logger.Error(fmt.Sprintf("something went wrong, stopping orchestrator processes: %v", err))
		return err
	}

	return nil
}

func NewManager(config *config.Config, logger log.Logger, server webserver.Server, db *storage.DB) Manager {
	return &manager{
		serverShutdownTimer: config.ServerShutdownTimer,
		logger:              logger,
		server:              server,
		db:                  db,
		sharedAuthToken:     config.SharedAuthToken,
	}
}
