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
	ProvisionWorkspace(ctx context.Context, repo string, revision string, stack string) (string, error)
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

func (m *manager) ProvisionWorkspace(ctx context.Context, repo string, revision string, stack string) (string, error) {
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
	repoDirPath := filepath.Join(parentDir, fmt.Sprintf("terraplane-workspace-%s-%s-%s", sanitizedRepo, revision, stack))

	if _, err := os.Stat(repoDirPath); err == nil {
		if workspaceReady(repoDirPath) {
			m.logger.Info(
				"Using existing workspace",
				"repo", repo,
				"revision", revision,
				"stack", stack,
				"path", repoDirPath,
			)
			m.workingDir = repoDirPath
			return repoDirPath, nil
		}

		m.logger.Info(
			"Removing incomplete workspace",
			"repo", repo,
			"revision", revision,
			"stack", stack,
			"path", repoDirPath,
		)
		if err := os.RemoveAll(repoDirPath); err != nil {
			return "", fmt.Errorf("failed to remove incomplete workspace %q: %w", repoDirPath, err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to stat workspace directory %q: %w", repoDirPath, err)
	}

	err := os.Mkdir(repoDirPath, 0o755)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(repoDirPath)
		}
	}()

	ok, err = m.cloneRepo(ctx, repo, revision, repoDirPath)
	if err != nil {
		return "", fmt.Errorf("failed to clone repository %s at revision %s: %w", repo, revision, err)
	}

	m.workingDir = repoDirPath
	return repoDirPath, nil
}

func (m *manager) cloneRepo(ctx context.Context, repo string, revision string, repoDirPath string) (bool, error) {
	host := scmHost(repo)
	knownHostsPath := filepath.Join(repoDirPath, "known_hosts")
	if err := m.scanHostKeys(ctx, host, knownHostsPath); err != nil {
		return false, err
	}

	gitSSH := fmt.Sprintf(
		"ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s",
		m.sshKeyPath,
		knownHostsPath,
	)
	repoURL := fmt.Sprintf("git@github.com:%s.git", repo)

	if err := m.runGit(ctx, repoDirPath, gitSSH, "init"); err != nil {
		return false, fmt.Errorf("failed to initialize git repository: %w", err)
	}
	if err := m.runGit(ctx, repoDirPath, gitSSH, "remote", "add", "origin", repoURL); err != nil {
		return false, fmt.Errorf("failed to add git remote: %w", err)
	}
	if err := m.runGit(ctx, repoDirPath, gitSSH, "fetch", "--depth", "1", "origin", revision); err != nil {
		return false, fmt.Errorf("failed to fetch revision %s from %s: %w", revision, repo, err)
	}
	if err := m.runGit(ctx, repoDirPath, gitSSH, "checkout", revision); err != nil {
		return false, fmt.Errorf("failed to checkout revision %s: %w", revision, err)
	}

	m.workingDir = repoDirPath
	return true, nil
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

func workspaceReady(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "terraplane.yaml")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	return true
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
