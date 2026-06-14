package agent

import (
	"context"
	"fmt"
	"time"

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
}

func (o *manager) Start(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
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
		logger:              logger,
	}
}
