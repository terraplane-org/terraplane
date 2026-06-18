package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyzjace/terraplane/internal/process"
	"github.com/xyzjace/terraplane/pkg/log"
)

type Manager interface {
	ProvisionWorkspace(ctx context.Context, repo string, revision string) (string, error)
	RemoveWorkspace(ctx context.Context) error
}

type manager struct {
	logger     log.Logger
	sshKeyPath string
	workDir    string
	workingDir string
}

func NewManager(logger log.Logger, sshKeyPath, workDir string) Manager {
	return &manager{logger: logger, sshKeyPath: sshKeyPath, workDir: workDir}
}

func (m *manager) ProvisionWorkspace(ctx context.Context, repo string, revision string) (string, error) {
	if m.sshKeyPath == "" {
		return "", fmt.Errorf("AGENT_SCM_SSH_KEY_PATH is not configured; SSH is required to clone private repositories")
	}
	if _, err := os.Stat(m.sshKeyPath); err != nil {
		return "", fmt.Errorf("SSH key not found at %q: %w", m.sshKeyPath, err)
	}

	parentDir := m.workDir
	if parentDir != "" {
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return "", fmt.Errorf("failed to create work directory %q: %w", parentDir, err)
		}
	}

	sanitizedRepo := strings.ReplaceAll(repo, "/", "-")
	tmpDir, err := os.MkdirTemp(parentDir, fmt.Sprintf("terraplane-workspace-%s-", sanitizedRepo))
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	host := scmHost(repo)
	knownHostsPath := filepath.Join(tmpDir, "known_hosts")
	if err := m.scanHostKeys(ctx, host, knownHostsPath); err != nil {
		return "", err
	}

	gitSSH := fmt.Sprintf(
		"ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s",
		m.sshKeyPath,
		knownHostsPath,
	)
	repoURL := fmt.Sprintf("git@github.com:%s.git", repo)

	if err := m.runGit(ctx, tmpDir, gitSSH, "init"); err != nil {
		return "", fmt.Errorf("failed to initialize git repository: %w", err)
	}
	if err := m.runGit(ctx, tmpDir, gitSSH, "remote", "add", "origin", repoURL); err != nil {
		return "", fmt.Errorf("failed to add git remote: %w", err)
	}
	if err := m.runGit(ctx, tmpDir, gitSSH, "fetch", "--depth", "1", "origin", revision); err != nil {
		return "", fmt.Errorf("failed to fetch revision %s from %s: %w", revision, repo, err)
	}
	if err := m.runGit(ctx, tmpDir, gitSSH, "checkout", revision); err != nil {
		return "", fmt.Errorf("failed to checkout revision %s: %w", revision, err)
	}

	ok = true
	m.workingDir = tmpDir
	return tmpDir, nil
}

func (m *manager) scanHostKeys(ctx context.Context, host, path string) error {
	result, err := process.Run(ctx, process.Command{
		Name: "ssh-keyscan",
		Args: []string{host},
	})
	if err != nil {
		return fmt.Errorf("failed to run ssh-keyscan for %s: %w", host, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("ssh-keyscan for %s failed with exit code %d: %s", host, result.ExitCode, process.Output(result))
	}
	if strings.TrimSpace(result.Stdout) == "" {
		return fmt.Errorf("ssh-keyscan for %s returned no host keys", host)
	}
	if err := os.WriteFile(path, []byte(result.Stdout), 0o600); err != nil {
		return fmt.Errorf("failed to write known hosts file %q: %w", path, err)
	}
	return nil
}

func (m *manager) runGit(ctx context.Context, dir, gitSSH string, args ...string) error {
	result, err := process.Run(ctx, process.Command{
		Name: "git",
		Args: args,
		Dir:  dir,
		Env:  []string{"GIT_SSH_COMMAND=" + gitSSH},
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git %s exited with code %d: %s", strings.Join(args, " "), result.ExitCode, process.Output(result))
	}
	return nil
}

func scmHost(repo string) string {
	// TODO: Implement this properly. It requires a feed of the host path from the orchestrator
	_ = repo
	return "github.com"
}

func (m *manager) RemoveWorkspace(ctx context.Context) error {
	if m.workingDir == "" {
		return nil
	}
	err := os.RemoveAll(m.workingDir)
	if err != nil {
		return fmt.Errorf("failed to remove workspace: %w", err)
	}
	m.workingDir = ""
	return nil
}
