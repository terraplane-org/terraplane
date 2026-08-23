package process

import (
	"context"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	t.Parallel()

	result, err := OSRunner{}.Run(context.Background(), Command{Name: "true"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestRunFailure(t *testing.T) {
	t.Parallel()

	result, err := OSRunner{}.Run(context.Background(), Command{Name: "false"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatal("ExitCode = 0, want non-zero")
	}
}

func TestRunStreamsCombinedOutput(t *testing.T) {
	t.Parallel()

	buf := NewStreamBuffer()
	result, err := OSRunner{}.Run(context.Background(), Command{
		Name:   "sh",
		Args:   []string{"-c", "printf out; printf err >&2"},
		Output: buf,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Stdout != "out" {
		t.Fatalf("Stdout = %q, want out", result.Stdout)
	}
	if result.Stderr != "err" {
		t.Fatalf("Stderr = %q, want err", result.Stderr)
	}
	if got := buf.Snapshot(); got != "outerr" && got != "errout" {
		t.Fatalf("Snapshot() = %q, want combined stdout/stderr", got)
	}
}

func TestStreamBufferSnapshot(t *testing.T) {
	t.Parallel()
	buf := NewStreamBuffer()
	if _, err := buf.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if got := buf.Snapshot(); got != "ab" {
		t.Fatalf("Snapshot() = %q", got)
	}
}

func TestOutput(t *testing.T) {
	t.Parallel()

	if got := Output(Result{Stdout: "out"}); got != "out" {
		t.Fatalf("Output(stdout) = %q, want out", got)
	}
	if got := Output(Result{Stderr: "err"}); got != "err" {
		t.Fatalf("Output(stderr) = %q, want err", got)
	}
	if got := Output(Result{Stdout: "out", Stderr: "err"}); got != "err" {
		t.Fatalf("Output(both) = %q, want err", got)
	}
}
