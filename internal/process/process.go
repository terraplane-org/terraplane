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
	Name   string
	Args   []string
	Dir    string
	Env    []string
	Output io.Writer
}

// StreamBuffer is a thread-safe writer that snapshots combined process output.
type StreamBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func NewStreamBuffer() *StreamBuffer {
	return &StreamBuffer{}
}

func (s *StreamBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *StreamBuffer) Snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
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
	if cmd.Output != nil {
		c.Stdout = io.MultiWriter(&stdout, cmd.Output)
		c.Stderr = io.MultiWriter(&stderr, cmd.Output)
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
