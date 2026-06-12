package orchestrator

import (
	"fmt"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
)

type Orchestrator interface{}

type orchestrator struct {
	logger log.Logger
}

func NewOrchestrator(config *config.Config, logger log.Logger) Orchestrator {
	fmt.Printf("Config: %+v\n", config)
	return &orchestrator{
		logger: logger,
	}
}
