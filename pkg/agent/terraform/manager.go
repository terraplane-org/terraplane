package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type Manager interface {
	RunPlan(ctx context.Context, workspaceDir, stackName, planFlags string) (string, error)
	RunApply(ctx context.Context, workspaceDir, stackName string) (string, error)
}

//go:generate mockgen -source=manager.go -destination=mock_terraform/mock_manager.go -package=mock_terraform

type manager struct {
	logger         log.Logger
	defaultVersion string
	versionManager VersionManager
	runner         Runner
}

func NewManager(cfg *config.Config, logger log.Logger) Manager {
	return NewManagerWith(
		logger,
		cfg.AgentDefaultTerraformVersion,
		NewVersionManager(logger, cfg.AgentTerraformBinDir),
		NewRunner(),
	)
}

// NewManagerWith constructs a Manager with explicit dependencies (useful in tests).
func NewManagerWith(logger log.Logger, defaultVersion string, versionManager VersionManager, runner Runner) Manager {
	return &manager{
		logger:         logger,
		defaultVersion: defaultVersion,
		versionManager: versionManager,
		runner:         runner,
	}
}

func (m *manager) RunPlan(ctx context.Context, workspaceDir, stackName, planFlags string) (string, error) {
	terraformDir, version, err := m.resolveStack(workspaceDir, stackName)
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

func (m *manager) RunApply(ctx context.Context, workspaceDir, stackName string) (string, error) {
	terraformDir, version, err := m.resolveStack(workspaceDir, stackName)
	if err != nil {
		return "", err
	}

	terraformBin, err := m.versionManager.Ensure(ctx, version)
	if err != nil {
		return "", err
	}

	return m.runner.Apply(ctx, terraformBin, terraformDir)
}

func (m *manager) resolveStack(workspaceDir, stackName string) (terraformDir, version string, err error) {
	file, err := os.ReadFile(filepath.Join(workspaceDir, "terraplane.yaml"))
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

	terraformDir, err = resolveStackDir(workspaceDir, stack.Dir)
	if err != nil {
		return "", "", fmt.Errorf("stack %q dir %q: %w", stackName, stack.Dir, err)
	}
	return terraformDir, version, nil
}

// resolveStackDir joins stackDir onto workspaceDir, rejecting any path that
// escapes the workspace (including via ".." or symlinks). os.Root is used the
// same way workspace provisioning confines AGENT_WORK_DIR.
func resolveStackDir(workspaceDir, stackDir string) (string, error) {
	rel := filepath.Clean(stackDir)
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("must be relative to the workspace")
	}

	root, err := os.OpenRoot(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to open workspace: %w", err)
	}
	defer func() { _ = root.Close() }()

	info, err := root.Stat(rel)
	if err != nil {
		return "", fmt.Errorf("path escapes workspace or does not exist: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("is not a directory")
	}

	return filepath.Join(workspaceDir, rel), nil
}
