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
