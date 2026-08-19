package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/handlers"
	"github.com/xyzjace/terraplane/pkg/agent/orchestrator"
	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
	"github.com/xyzjace/terraplane/pkg/log"
)

const reconnectInterval = 5 * time.Second

type Manager interface {
	Start(ctx context.Context) error
}

type manager struct {
	logger             log.Logger
	config             *config.Config
	workspaceManager   workspace.Manager
	terraformManager   terraform.Manager
	orchestratorClient orchestrator.Client
}

func (o *manager) Start(ctx context.Context) error {
	if o.config.AgentID == "" {
		o.logger.Error("Agent ID is not set. Please set the AGENT_ID environment variable.")
		return fmt.Errorf("agent ID is not set")
	}

	if o.config.SharedAuthToken == "" {
		o.logger.Error("Shared auth token is not set. Please set the SHARED_AUTH_TOKEN environment variable.")
		return fmt.Errorf("shared auth token is not set")
	}

	o.logger.Info("Starting agent poll loop...", "agent_id", o.config.AgentID, "poll_interval", o.config.AgentPollInterval)

	h := handlers.New(o.logger, o.config, o.workspaceManager, o.terraformManager, o.orchestratorClient)
	p := newPoller(o.config, o.logger, h, o.orchestratorClient)

	for {
		err := p.run(ctx)
		if ctx.Err() != nil {
			o.logger.Info("Agent stopped")
			return nil
		}

		if err != nil {
			o.logger.Warn("Agent poll loop error; restarting", "error", err, "after", reconnectInterval)
		}

		select {
		case <-ctx.Done():
			o.logger.Info("Agent stopped")
			return nil
		case <-time.After(reconnectInterval):
		}
	}
}

func NewManager(
	config *config.Config,
	logger log.Logger,
	workspaceManager workspace.Manager,
	terraformManager terraform.Manager,
	orchestratorClient orchestrator.Client,
) Manager {
	return &manager{
		config:             config,
		logger:             logger,
		workspaceManager:   workspaceManager,
		terraformManager:   terraformManager,
		orchestratorClient: orchestratorClient,
	}
}
