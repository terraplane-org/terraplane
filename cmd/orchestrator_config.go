package cmd

import (
	"strings"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestratorconfig"
)

func loadOrchestratorConfig(logger log.Logger, cfg *config.Config) (*orchestratorconfig.ConfigFile, error) {
	path := strings.TrimSpace(cfg.OrchestratorConfigPath)
	file, err := orchestratorconfig.LoadFile(path)
	if err != nil {
		return nil, err
	}
	if file == nil && path != "" {
		logger.Info("Orchestrator config file is not present; plan gates are off", "path", path)
	}
	return file, nil
}
