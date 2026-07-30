package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
	"github.com/xyzjace/terraplane/pkg/log"
	"golang.org/x/sync/errgroup"
)

const reconnectInterval = 5 * time.Second

type Manager interface {
	Start(ctx context.Context) error
}

type manager struct {
	logger           log.Logger
	orchestratorURL  string
	id               string
	sharedAuthToken  string
	workspaceManager workspace.Manager
	terraformManager terraform.Manager
}

func (o *manager) Start(ctx context.Context) error {
	if o.id == "" {
		o.logger.Error("Agent ID is not set. Please set the AGENT_ID environment variable.")
		return fmt.Errorf("agent ID is not set")
	}

	if o.sharedAuthToken == "" {
		o.logger.Error("Shared auth token is not set. Please set the SHARED_AUTH_TOKEN environment variable.")
		return fmt.Errorf("shared auth token is not set")
	}

	o.logger.Info("Starting agent...")

	for {
		err := o.runOnce(ctx)
		if ctx.Err() != nil {
			o.logger.Info("Agent stopped")
			return nil
		}

		if err != nil {
			o.logger.Warn("Agent disconnected; reconnecting", "error", err, "after", reconnectInterval)
		} else {
			o.logger.Debug("Agent disconnected; reconnecting", "after", reconnectInterval)
		}

		select {
		case <-ctx.Done():
			o.logger.Info("Agent stopped")
			return nil
		case <-time.After(reconnectInterval):
		}
	}
}

func (o *manager) runOnce(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, o.orchestratorURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{
			"Authorization": {auth.BearerHeader(o.sharedAuthToken)},
		},
	})
	if err != nil {
		return fmt.Errorf("dial orchestrator: %w", err)
	}

	session := NewSession(o.id, conn, o.logger, o.workspaceManager, o.terraformManager)

	if err := session.Hello(ctx); err != nil {
		session.CloseNow()
		return fmt.Errorf("write agent hello: %w", err)
	}

	group, gCtx := errgroup.WithContext(ctx)
	runDone := make(chan struct{})

	group.Go(func() error {
		defer close(runDone)
		defer session.Close(websocket.StatusNormalClosure, "")
		return session.Run(gCtx)
	})

	group.Go(func() error {
		select {
		case <-gCtx.Done():
			session.Close(websocket.StatusNormalClosure, "shutting down")
			return nil
		case <-runDone:
			return nil
		}
	})

	if err := group.Wait(); err != nil {
		if gCtx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

func NewManager(
	config *config.Config,
	logger log.Logger,
	workspaceManager workspace.Manager,
	terraformManager terraform.Manager,
) Manager {
	return &manager{
		id:               config.AgentID,
		orchestratorURL:  config.AgentOrchestratorURL,
		logger:           logger,
		sharedAuthToken:  config.SharedAuthToken,
		workspaceManager: workspaceManager,
		terraformManager: terraformManager,
	}
}
