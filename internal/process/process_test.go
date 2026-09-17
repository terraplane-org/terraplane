package process

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestRunLiveSeesOutputBeforeExit(t *testing.T) {
	t.Parallel()

	live := &Buffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seen := make(chan struct{})
	go func() {
		for {
			if strings.Contains(live.String(), "hello") {
				close(seen)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		_, err := OSRunner{}.Run(context.Background(), Command{
			Name: "sh",
			Args: []string{"-c", "echo hello; sleep 1; echo done"},
			Live: live,
		})
		errCh <- err
	}()

	select {
	case <-seen:
	case err := <-errCh:
		t.Fatalf("command finished before live output was visible: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for live output")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(live.String(), "done") {
		t.Fatalf("live = %q, want to contain done", live.String())
	}
}
