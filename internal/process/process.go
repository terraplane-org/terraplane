package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func Run(ctx context.Context, cmd Command) (Result, error) {
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	if cmd.Dir != "" {
		c.Dir = cmd.Dir
	}
	if len(cmd.Env) > 0 {
		c.Env = append(os.Environ(), cmd.Env...)
	}

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	result := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err == nil {
		result.ExitCode = 0
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}

	return result, fmt.Errorf("failed to run %s: %w", cmd.Name, err)
}

func Output(result Result) string {
	if result.Stderr != "" {
		return result.Stderr
	}
	return result.Stdout
}
