package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	hcversion "github.com/hashicorp/go-version"
	install "github.com/hashicorp/hc-install"
	"github.com/hashicorp/hc-install/fs"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/hc-install/src"
	"github.com/xyzjace/terraplane/pkg/log"
	"golang.org/x/sync/singleflight"
)

var terraformInstallGroup singleflight.Group

type VersionManager interface {
	Ensure(ctx context.Context, version string) (string, error)
}

type versionManager struct {
	logger log.Logger
	binDir string
}

func NewVersionManager(logger log.Logger, binDir string) VersionManager {
	if binDir == "" {
		binDir = filepath.Join(os.TempDir(), "terraplane-terraform")
	}
	return &versionManager{logger: logger, binDir: binDir}
}

func (m *versionManager) Ensure(ctx context.Context, version string) (string, error) {
	v, err := parseTerraformVersion(version)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(m.binDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create terraform bin directory %q: %w", m.binDir, err)
	}

	versionDir := filepath.Join(m.binDir, v.String())
	execPath := filepath.Join(versionDir, product.Terraform.BinaryName())

	if path, err := existingTerraformBinary(execPath); err == nil {
		m.logger.Info("Using terraform binary", "version", v.String(), "path", path)
		return path, nil
	}

	result, err, _ := terraformInstallGroup.Do(versionDir, func() (any, error) {
		if path, err := existingTerraformBinary(execPath); err == nil {
			return path, nil
		}

		if err := os.MkdirAll(versionDir, 0o755); err != nil {
			return "", fmt.Errorf("failed to create terraform version directory %q: %w", versionDir, err)
		}

		installer := install.NewInstaller()
		path, err := installer.Ensure(ctx, []src.Source{
			&fs.ExactVersion{
				Product:    product.Terraform,
				Version:    v,
				ExtraPaths: []string{versionDir},
			},
			&releases.ExactVersion{
				Product:    product.Terraform,
				Version:    v,
				InstallDir: versionDir,
			},
		})
		if err != nil {
			return "", err
		}
		return path, nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure terraform %s: %w", v, err)
	}

	execPath = result.(string)
	m.logger.Info("Using terraform binary", "version", v.String(), "path", execPath)
	return execPath, nil
}

func existingTerraformBinary(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("terraform binary path %q is a directory", path)
	}
	return path, nil
}

func parseTerraformVersion(version string) (*hcversion.Version, error) {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return nil, fmt.Errorf("terraform version is required")
	}

	v, err := hcversion.NewVersion(version)
	if err != nil {
		return nil, fmt.Errorf("invalid terraform version %q: %w", version, err)
	}
	return v, nil
}
