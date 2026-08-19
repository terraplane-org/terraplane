package agentsession

import (
	"time"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
)

type Factory interface {
	New(id string, conn *websocket.Conn) Session
}

// HeartbeatConfig controls orchestrator→agent ping/pong. Interval <= 0 disables heartbeats.
type HeartbeatConfig struct {
	Interval         time.Duration
	Timeout          time.Duration
	MissedHeartbeats int
}

func HeartbeatConfigFrom(cfg *config.Config) HeartbeatConfig {
	return HeartbeatConfig{
		Interval:         cfg.OrchestratorAgentPingInterval,
		Timeout:          cfg.OrchestratorAgentPongTimeout,
		MissedHeartbeats: cfg.OrchestratorAgentMissedHeartbeats,
	}
}

type factory struct {
	logger         log.Logger
	registry       Registry
	jobRepository  repository.JobRepository
	lockRepository repository.LockRepository
	scmPublisher   scm.Publisher
	jobService     services.JobService
	heartbeat      HeartbeatConfig
}

func NewFactory(
	logger log.Logger,
	registry Registry,
	jobRepository repository.JobRepository,
	lockRepository repository.LockRepository,
	scmPublisher scm.Publisher,
	jobService services.JobService,
	cfg *config.Config,
) Factory {
	return &factory{
		logger:         logger,
		registry:       registry,
		jobRepository:  jobRepository,
		lockRepository: lockRepository,
		scmPublisher:   scmPublisher,
		jobService:     jobService,
		heartbeat:      HeartbeatConfigFrom(cfg),
	}
}

func (f *factory) New(id string, conn *websocket.Conn) Session {
	missed := f.heartbeat.MissedHeartbeats
	if missed <= 0 {
		missed = 2
	}
	return &session{
		id:               id,
		conn:             conn,
		logger:           f.logger,
		registry:         f.registry,
		jobRepository:    f.jobRepository,
		lockRepository:   f.lockRepository,
		scmPublisher:     f.scmPublisher,
		jobService:       f.jobService,
		pingInterval:     f.heartbeat.Interval,
		pongTimeout:      f.heartbeat.Timeout,
		missedHeartbeats: missed,
		pongCh:           make(chan struct{}, 1),
	}
}
