//go:generate go run -mod=mod github.com/goforj/wire/cmd/wire

//go:build wireinject
// +build wireinject

package cmd

import (
	"github.com/goforj/wire"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/logging"
	"github.com/xyzjace/terraplane/pkg/agent"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/orchestrator"
	"github.com/xyzjace/terraplane/pkg/scm/github"
	"github.com/xyzjace/terraplane/pkg/webserver"
)

var observableSet = wire.NewSet(logging.NewLogger)

func InitializeOrchestrator() (orchestrator.Manager, error) {

	wire.Build(observableSet, orchestrator.NewManager, config.NewConfig, webserver.NewServer, webserver.NewHandler, github.NewClient, github.NewProvider, agentsession.NewRegistry, agentsession.NewFactory)
	return nil, nil
}

func InitializeAgent() (agent.Manager, error) {

	wire.Build(observableSet, agent.NewManager, config.NewConfig)
	return nil, nil
}
