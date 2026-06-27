package agentsession

import (
	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
)

type Factory interface {
	New(id string, conn *websocket.Conn) Session
}

type factory struct {
	logger         log.Logger
	registry       Registry
	jobRepository  repository.JobRepository
	lockRepository repository.LockRepository
}

func NewFactory(logger log.Logger, registry Registry, jobRepository repository.JobRepository, lockRepository repository.LockRepository) Factory {
	return &factory{
		logger:         logger,
		registry:       registry,
		jobRepository:  jobRepository,
		lockRepository: lockRepository,
	}
}

func (f *factory) New(id string, conn *websocket.Conn) Session {
	return &session{
		id:             id,
		conn:           conn,
		logger:         f.logger,
		registry:       f.registry,
		jobRepository:  f.jobRepository,
		lockRepository: f.lockRepository,
	}
}
