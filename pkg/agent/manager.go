package agent

import (
	"context"
	"fmt"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"golang.org/x/sync/errgroup"
)

type Manager interface {
	Start(ctx context.Context) error
}

type manager struct {
	logger          log.Logger
	orchestratorURL string
	id              string
}

func (o *manager) Start(ctx context.Context) error {
	if o.id == "" {
		o.logger.Error("Agent ID is not set. Please set the AGENT_ID environment variable.")
		return fmt.Errorf("agent ID is not set")
	}

	o.logger.Info("Starting agent...")

	conn, _, err := websocket.Dial(ctx, o.orchestratorURL, nil)
	if err != nil {
		return fmt.Errorf("dial orchestrator: %w", err)
	}

	session := NewSession(o.id, conn, o.logger)

	if err := session.Hello(ctx); err != nil {
		session.CloseNow()
		return fmt.Errorf("write agent hello: %w", err)
	}

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		defer session.Close(websocket.StatusNormalClosure, "")
		return session.Run(ctx)
	})

	group.Go(func() error {
		<-ctx.Done()
		session.Close(websocket.StatusNormalClosure, "shutting down")
		return nil
	})

	if err := group.Wait(); err != nil {
		if ctx.Err() != nil {
			o.logger.Info("Agent stopped")
			return nil
		}
		o.logger.Error("Agent stopped with error", "error", err)
		return err
	}

	o.logger.Info("Agent stopped")
	return nil
}

func NewManager(config *config.Config, logger log.Logger) Manager {
	return &manager{
		id:              config.AgentID,
		orchestratorURL: config.AgentOrchestratorURL,
		logger:          logger,
	}
}
