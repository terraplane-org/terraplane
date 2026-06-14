package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"golang.org/x/sync/errgroup"
)

type Manager interface {
	Start(ctx context.Context) error
}

type manager struct {
	clientShutdownTimer time.Duration
	logger              log.Logger
	orchestratorURL     string
}

func (o *manager) Start(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		c, _, err := websocket.Dial(ctx, o.orchestratorURL, nil)
		if err != nil {
			return fmt.Errorf("failed to dial orchestrator: %w", err)
		}
		defer c.CloseNow()

		err = wsjson.Write(ctx, c, "hi")
		if err != nil {
			return fmt.Errorf("failed to write to orchestrator: %w", err)
		}

		c.Close(websocket.StatusNormalClosure, "")
		return nil
	})

	group.Go(func() error {
		<-ctx.Done()
		return nil
	})

	if err := group.Wait(); err != nil {
		o.logger.Error(fmt.Sprintf("something went wrong, stopping orchestrator processes: %v", err))
		return err
	}

	return nil
}

func NewManager(config *config.Config, logger log.Logger) Manager {
	return &manager{
		clientShutdownTimer: config.AgentClientShutdownTimer,
		orchestratorURL:     config.AgentOrchestratorURL,
		logger:              logger,
	}
}
