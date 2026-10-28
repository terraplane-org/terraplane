package cmd

import (
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/orchestratorconfig"
)

func loadOrchestratorConfig(cfg *config.Config) (*orchestratorconfig.ConfigFile, error) {
	return orchestratorconfig.LoadFile(cfg.OrchestratorConfigPath)
}
