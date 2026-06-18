package process

import (
	"context"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	t.Parallel()

	result, err := Run(context.Background(), Command{Name: "true"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestRunFailure(t *testing.T) {
	t.Parallel()

	result, err := Run(context.Background(), Command{Name: "false"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatal("ExitCode = 0, want non-zero")
	}
}
