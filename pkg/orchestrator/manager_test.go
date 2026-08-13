package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
)

type stubSchema struct {
	err error
}

func (s stubSchema) RequireCurrentSchema(context.Context) error { return s.err }

type stubServer struct {
	startErr    error
	shutdownErr error
	started     chan struct{}
	shutdown    chan struct{}
}

func (s *stubServer) Start(ctx context.Context) error {
	if s.started != nil {
		close(s.started)
	}
	if s.startErr != nil {
		return s.startErr
	}
	<-ctx.Done()
	return nil
}

func (s *stubServer) Shutdown(context.Context) error {
	if s.shutdown != nil {
		close(s.shutdown)
	}
	return s.shutdownErr
}

type stubResults struct{}

func (stubResults) Complete(context.Context, string, string, bool, string, string) error { return nil }
func (stubResults) FailExpired(context.Context) error                                   { return nil }

func testReaper() *services.LeaseReaper {
	return services.NewLeaseReaper(log.Noop(), stubResults{}, time.Hour)
}

func TestStartMissingAuthToken(t *testing.T) {
	m := orchestrator.NewManager(
		&config.Config{SharedAuthToken: "", ServerShutdownTimer: time.Second},
		log.Noop(),
		&stubServer{},
		stubSchema{},
		testReaper(),
	)
	err := m.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "SHARED_AUTH_TOKEN")
}

func TestStartSchemaCheckFailure(t *testing.T) {
	m := orchestrator.NewManager(
		&config.Config{SharedAuthToken: "secret", ServerShutdownTimer: time.Second},
		log.Noop(),
		&stubServer{},
		stubSchema{err: errors.New("migrations pending")},
		testReaper(),
	)
	err := m.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "migrations pending")
}

func TestStartAndShutdownOnCancel(t *testing.T) {
	started := make(chan struct{})
	shutdown := make(chan struct{})
	srv := &stubServer{started: started, shutdown: shutdown}

	m := orchestrator.NewManager(
		&config.Config{SharedAuthToken: "secret", ServerShutdownTimer: time.Second},
		log.Noop(),
		srv,
		stubSchema{},
		testReaper(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server start")
	}
	cancel()

	select {
	case <-shutdown:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server shutdown")
	}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}

func TestStartPropagatesServerError(t *testing.T) {
	m := orchestrator.NewManager(
		&config.Config{SharedAuthToken: "secret", ServerShutdownTimer: time.Second},
		log.Noop(),
		&stubServer{startErr: errors.New("bind failed")},
		stubSchema{},
		testReaper(),
	)

	err := m.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "bind failed")
}
