package webserver

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
)

func TestHandleCommandUnhandledKind(t *testing.T) {
	// Intention: unknown kinds after parse are warned and ignored (no panic / no service call).
	h := &handler{logger: log.Noop()}
	h.handleCommand(context.Background(), command.Command{Kind: command.Kind("weird")})
}

func TestServerStartReturnsNilOnContextCancel(t *testing.T) {
	cfg := &config.Config{
		OrchestratorListenAddress: "127.0.0.1",
		OrchestratorListenPort:    0,
	}
	srv := NewServer(cfg, log.Noop(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start after cancel")
	}
}

func TestServerStartReturnsNilOnErrServerClosed(t *testing.T) {
	cfg := &config.Config{
		OrchestratorListenAddress: "127.0.0.1",
		OrchestratorListenPort:    0,
	}
	srv := NewServer(cfg, log.Noop(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	done := make(chan error, 1)
	go func() { done <- srv.Start(context.Background()) }()

	time.Sleep(20 * time.Millisecond)
	require.NoError(t, srv.Shutdown(context.Background()))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start after Shutdown")
	}
}

func TestServerStartPropagatesListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	cfg := &config.Config{
		OrchestratorListenAddress: "127.0.0.1",
		OrchestratorListenPort:    port,
	}
	srv := NewServer(cfg, log.Noop(), nil)
	err = srv.Start(context.Background())
	require.Error(t, err)
}
