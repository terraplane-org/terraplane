package agentsession

import (
	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/log"
)

type Factory interface {
	New(id string, conn *websocket.Conn) Session
}

type factory struct {
	logger   log.Logger
	registry Registry
}

func NewFactory(logger log.Logger, registry Registry) Factory {
	return &factory{
		logger:   logger,
		registry: registry,
	}
}

func (f *factory) New(id string, conn *websocket.Conn) Session {
	return &session{
		id:       id,
		conn:     conn,
		logger:   f.logger,
		registry: f.registry,
	}
}
