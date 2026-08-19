package agent

import (
	"context"
	"time"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/orchestrator"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
)

//go:generate mockgen -destination=mock_agent/mock_dispatcher.go -package=mock_agent github.com/xyzjace/terraplane/pkg/agent Dispatcher

// Dispatcher runs a command and signals completion via the done channel.
type Dispatcher interface {
	Dispatch(ctx context.Context, cmd *command.Command, done chan<- struct{})
}

type poller struct {
	logger             log.Logger
	config             *config.Config
	dispatcher         Dispatcher
	orchestratorClient orchestrator.Client
}

func newPoller(
	cfg *config.Config,
	logger log.Logger,
	dispatcher Dispatcher,
	orchestratorClient orchestrator.Client,
) *poller {
	return &poller{
		logger:             logger,
		config:             cfg,
		dispatcher:         dispatcher,
		orchestratorClient: orchestratorClient,
	}
}

// run polls for jobs until ctx is cancelled or a fatal error occurs.
// A claim error causes run to return so the manager can reconnect/retry.
func (p *poller) run(ctx context.Context) error {
	ticker := time.NewTicker(p.config.AgentPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cmd, err := p.orchestratorClient.ClaimJob(ctx, p.config.AgentID)
			if err != nil {
				p.logger.Error("Failed to claim job from orchestrator", "agent_id", p.config.AgentID, "error", err)
				return err
			}
			if cmd == nil {
				p.logger.Debug("No pending jobs", "agent_id", p.config.AgentID)
				continue
			}

			p.executeJob(ctx, cmd)
		}
	}
}

func (p *poller) executeJob(ctx context.Context, cmd *command.Command) {
	jobID := cmd.JobID()

	if err := p.orchestratorClient.Ack(ctx, jobID, p.config.AgentID); err != nil {
		p.logger.Error("Failed to ack job", "job_id", jobID, "agent_id", p.config.AgentID, "error", err)
		return
	}

	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()
	go heartbeat(hbCtx, p.logger, p.orchestratorClient, jobID, p.config.AgentID, p.config.AgentHeartbeatInterval)

	// Dispatch is fire-and-forget internally, but we wait for it inline here
	// so the heartbeat goroutine stays alive for the duration of the job.
	// handlers.Dispatch launches a goroutine and returns immediately, so we
	// need a done channel to know when to cancel the heartbeat.
	done := make(chan struct{})
	p.dispatcher.Dispatch(ctx, cmd, done)
	<-done
}

func heartbeat(ctx context.Context, logger log.Logger, client orchestrator.Client, jobID, agentID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := client.Heartbeat(ctx, jobID, agentID); err != nil {
				// Heartbeat failure is non-fatal — the reaper is the backstop for truly dead
				// agents. Log and keep trying; the job continues running.
				logger.Warn("Failed to send heartbeat", "job_id", jobID, "agent_id", agentID, "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
