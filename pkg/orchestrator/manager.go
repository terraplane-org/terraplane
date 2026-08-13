package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/webserver"
	"golang.org/x/sync/errgroup"
)

type Manager interface {
	Start(ctx context.Context) error
}

// SchemaChecker verifies the database schema is migrated before serving traffic.
type SchemaChecker interface {
	RequireCurrentSchema(ctx context.Context) error
}

type manager struct {
	serverShutdownTimer time.Duration
	logger              log.Logger
	server              webserver.Server
	schema              SchemaChecker
	sharedAuthToken     string
	leaseReaper         *services.LeaseReaper
}

func (o *manager) Start(ctx context.Context) error {
	if o.sharedAuthToken == "" {
		return fmt.Errorf("SHARED_AUTH_TOKEN is not configured")
	}

	if err := o.schema.RequireCurrentSchema(ctx); err != nil {
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
		return o.leaseReaper.Run(ctx)
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

func NewManager(
	cfg *config.Config,
	logger log.Logger,
	server webserver.Server,
	schema SchemaChecker,
	leaseReaper *services.LeaseReaper,
) Manager {
	return &manager{
		serverShutdownTimer: cfg.ServerShutdownTimer,
		logger:              logger,
		server:              server,
		schema:              schema,
		sharedAuthToken:     cfg.SharedAuthToken,
		leaseReaper:         leaseReaper,
	}
}
