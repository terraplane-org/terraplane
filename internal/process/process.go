package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
	Live io.Writer
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Buffer is a bytes.Buffer that is safe for concurrent Write and String.
type Buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Runner executes external commands. Production code uses OSRunner.
type Runner interface {
	Run(ctx context.Context, cmd Command) (Result, error)
}

// OSRunner runs commands via os/exec.
type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, cmd Command) (Result, error) {
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
	if cmd.Live != nil {
		c.Stdout = io.MultiWriter(&stdout, cmd.Live)
		c.Stderr = io.MultiWriter(&stderr, cmd.Live)
	}

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
