package agentsession

import (
	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
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
	scmPublisher   scm.Publisher
}

func NewFactory(logger log.Logger, registry Registry, jobRepository repository.JobRepository, lockRepository repository.LockRepository, scmPublisher scm.Publisher) Factory {
	return &factory{
		logger:         logger,
		registry:       registry,
		jobRepository:  jobRepository,
		lockRepository: lockRepository,
		scmPublisher:   scmPublisher,
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
		scmPublisher:   f.scmPublisher,
	}
}
