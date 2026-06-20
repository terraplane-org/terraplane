package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type Manager interface {
	RunPlan(ctx context.Context, stackName, planFlags string) (string, error)
}

type manager struct {
	logger         log.Logger
	workspaceDir   string
	defaultVersion string
	versionManager VersionManager
	runner         Runner
}

func NewManager(logger log.Logger, workspaceDir, terraformBinDir, defaultTerraformVersion string) Manager {
	return &manager{
		logger:         logger,
		workspaceDir:   workspaceDir,
		defaultVersion: defaultTerraformVersion,
		versionManager: NewVersionManager(logger, terraformBinDir),
		runner:         NewRunner(),
	}
}

func (m *manager) RunPlan(ctx context.Context, stackName, planFlags string) (string, error) {
	terraformDir, version, err := m.resolveStack(stackName)
	if err != nil {
		return "", err
	}

	terraformBin, err := m.versionManager.Ensure(ctx, version)
	if err != nil {
		return "", err
	}

	if err := m.runner.Init(ctx, terraformBin, terraformDir); err != nil {
		return "", err
	}

	return m.runner.Plan(ctx, terraformBin, terraformDir, planFlags)
}

func (m *manager) resolveStack(stackName string) (terraformDir, version string, err error) {
	file, err := os.ReadFile(filepath.Join(m.workspaceDir, "terraplane.yaml"))
	if err != nil {
		return "", "", fmt.Errorf("failed to read terraplane config: %w", err)
	}

	terraplaneConfig, err := terraplaneconfig.ParseConfigFile(file)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse terraplane config: %w", err)
	}

	var stack *terraplaneconfig.Stack
	for i := range terraplaneConfig.Stacks {
		if terraplaneConfig.Stacks[i].Name == stackName {
			stack = &terraplaneConfig.Stacks[i]
			break
		}
	}
	if stack == nil {
		return "", "", fmt.Errorf("stack %q not found in terraplane config", stackName)
	}

	version = stack.TerraformVersion
	if version == "" {
		version = m.defaultVersion
	}
	if version == "" {
		return "", "", fmt.Errorf("no terraform version configured for stack %q and AGENT_DEFAULT_TERRAFORM_VERSION is not set", stackName)
	}

	return filepath.Join(m.workspaceDir, stack.Dir), version, nil
}
