package storage

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed migrations/*
var embeddedMigrations embed.FS

type atlasMigrateStatus struct {
	Status    string `json:"Status"`
	Next      string `json:"Next"`
	Available []struct {
		Name    string `json:"Name"`
		Version string `json:"Version"`
	} `json:"Available"`
	Applied []struct {
		Version string `json:"Version"`
	} `json:"Applied"`
}

func (s atlasMigrateStatus) pendingMigrations() []string {
	applied := make(map[string]struct{}, len(s.Applied))
	for _, migration := range s.Applied {
		applied[migration.Version] = struct{}{}
	}

	var pending []string
	for _, migration := range s.Available {
		if _, ok := applied[migration.Version]; !ok {
			pending = append(pending, migration.Name)
		}
	}
	return pending
}

func materializeMigrationsDir() (dirURL string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "terraplane-migrations-*")
	if err != nil {
		return "", nil, fmt.Errorf("prepare embedded migrations: %w", err)
	}

	cleanup = func() { _ = os.RemoveAll(tmp) }

	err = fs.WalkDir(embeddedMigrations, "migrations", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		data, err := embeddedMigrations.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded migration %s: %w", path, err)
		}

		name := strings.TrimPrefix(path, "migrations/")
		dest := filepath.Join(tmp, name)
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write migration %s: %w", name, err)
		}
		return nil
	})
	if err != nil {
		cleanup()
		return "", nil, err
	}

	abs, err := filepath.Abs(tmp)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("resolve migrations directory: %w", err)
	}

	return "file://" + filepath.ToSlash(abs), cleanup, nil
}

func runAtlas(ctx context.Context, args ...string) (stdout string, err error) {
	cmd := exec.CommandContext(ctx, "atlas", args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		return "", atlasCommandError(args, stderrBuf.String(), err)
	}
	return stdoutBuf.String(), nil
}

func atlasCommandError(args []string, stderr string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf(
			"atlas CLI not found in PATH; install Atlas from https://atlasgo.io or run inside the terraplane container image",
		)
	}

	subcommand := "command"
	if len(args) > 0 {
		subcommand = args[0]
	}

	stderr = strings.TrimSpace(stderr)
	switch {
	case stderr != "":
		return fmt.Errorf("atlas %s failed: %s", subcommand, stderr)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("atlas %s canceled: %w", subcommand, err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("atlas %s timed out: %w", subcommand, err)
	default:
		return fmt.Errorf("atlas %s failed: %w", subcommand, err)
	}
}

func migrationStatus(ctx context.Context, databaseURL string) (atlasMigrateStatus, error) {
	dirURL, cleanup, err := materializeMigrationsDir()
	if err != nil {
		return atlasMigrateStatus{}, err
	}
	defer cleanup()

	out, err := runAtlas(ctx,
		"migrate", "status",
		"--dir", dirURL,
		"--url", databaseURL,
		"--format", "{{ json . }}",
	)
	if err != nil {
		return atlasMigrateStatus{}, fmt.Errorf("check database migration status: %w", err)
	}

	var status atlasMigrateStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &status); err != nil {
		return atlasMigrateStatus{}, fmt.Errorf(
			"parse atlas migration status output: %w",
			err,
		)
	}
	return status, nil
}

func applyMigrations(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return errDatabaseURLRequired()
	}

	dirURL, cleanup, err := materializeMigrationsDir()
	if err != nil {
		return err
	}
	defer cleanup()

	_, err = runAtlas(ctx,
		"migrate", "apply",
		"--dir", dirURL,
		"--url", databaseURL,
	)
	if err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}
	return nil
}

func verifyMigrations(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return errDatabaseURLRequired()
	}

	status, err := migrationStatus(ctx, databaseURL)
	if err != nil {
		return err
	}

	pending := status.pendingMigrations()
	if len(pending) == 0 && status.Status == "OK" {
		return nil
	}

	if len(pending) > 0 {
		return fmt.Errorf(
			"database schema is behind this application version: pending migration(s): %s; run `terraplane db migrate` or ensure the db-migrate init container completed successfully",
			strings.Join(pending, ", "),
		)
	}

	return fmt.Errorf(
		"database schema is not up to date (status %q, next %q); run `terraplane db migrate` or ensure the db-migrate init container completed successfully",
		status.Status,
		status.Next,
	)
}

func errDatabaseURLRequired() error {
	return fmt.Errorf(
		"DATABASE_URL is not set; configure a postgres or mysql connection string in the environment",
	)
}
