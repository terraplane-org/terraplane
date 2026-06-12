//go:generate go run -mod=mod github.com/goforj/wire/cmd/wire

//go:build wireinject
// +build wireinject

package cmd

import (
	"github.com/goforj/wire"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/logging"
	"github.com/xyzjace/terraplane/pkg/orchestrator"
)

var observableSet = wire.NewSet(logging.NewLogger)

func InitializeOrchestrator() (orchestrator.Orchestrator, error) {

	wire.Build(observableSet, orchestrator.NewOrchestrator, config.NewConfig)
	return nil, nil
}
