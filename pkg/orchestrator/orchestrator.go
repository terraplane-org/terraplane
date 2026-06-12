package orchestrator

import "github.com/xyzjace/terraplane/pkg/log"

type Orchestrator interface{}

type orchestrator struct {
	logger log.Logger
}

func NewOrchestrator(logger log.Logger) Orchestrator {
	return &orchestrator{
		logger: logger,
	}
}
