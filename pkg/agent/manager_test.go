package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentapi"
	"github.com/xyzjace/terraplane/pkg/log"
)

func TestStartRequiresAgentID(t *testing.T) {
	m := NewManager(&config.Config{}, log.Noop(), nil, nil)
	err := m.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent ID is not set")
}

func TestStartRequiresSharedAuthToken(t *testing.T) {
	m := NewManager(&config.Config{AgentID: "agent-a"}, log.Noop(), nil, nil)
	err := m.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "shared auth token is not set")
}

func TestNewManagerHeartbeatFallback(t *testing.T) {
	m := NewManager(&config.Config{
		AgentID:              "agent-a",
		SharedAuthToken:      "secret",
		OrchestratorJobLease: 90 * time.Second,
	}, log.Noop(), nil, nil).(*manager)
	require.Equal(t, 30*time.Second, m.heartbeatEvery)

	m = NewManager(&config.Config{
		AgentID:                "agent-a",
		SharedAuthToken:        "secret",
		AgentHeartbeatInterval: 12 * time.Second,
	}, log.Noop(), nil, nil).(*manager)
	require.Equal(t, 12*time.Second, m.heartbeatEvery)
}

func TestEnvelopeFromJob(t *testing.T) {
	plan, err := envelopeFromJob(&agentapi.Job{
		JobID: "job-1", Action: "plan", Repo: "acme/infra", PRNumber: 1,
		CommitSHA: "abc", StackName: "a", Dir: "stacks/a", PlanFlags: "-target=x",
	})
	require.NoError(t, err)
	require.Equal(t, "job-1", plan.GetJobId())
	require.Equal(t, "-target=x", plan.GetPlan().GetPlanFlags())

	apply, err := envelopeFromJob(&agentapi.Job{JobID: "job-2", Action: "apply", StackName: "a"})
	require.NoError(t, err)
	require.NotNil(t, apply.GetApply())

	_, err = envelopeFromJob(&agentapi.Job{JobID: "job-3", Action: "unlock"})
	require.Error(t, err)
}

func TestTickReportsUnsupportedAction(t *testing.T) {
	var result agentapi.Result
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/jobs/poll":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentapi.Job{JobID: "job-1", Action: "unlock"})
		case "/agent/jobs/job-1/result":
			_ = json.NewDecoder(r.Body).Decode(&result)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)

	m := &manager{
		logger:         log.Noop(),
		id:             "agent-a",
		client:         newOrchClient(srv.URL, "agent-a", "secret"),
		pollInterval:   time.Hour,
		heartbeatEvery: time.Hour,
	}
	worked, err := m.tick(context.Background(), nil)
	require.True(t, worked)
	require.Error(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Error, "unsupported job action")
}

func TestTickPollError(t *testing.T) {
	m := &manager{
		logger: log.Noop(),
		client: newOrchClient("http://127.0.0.1:1", "agent-a", "secret"),
	}
	worked, err := m.tick(context.Background(), nil)
	require.False(t, worked)
	require.Error(t, err)
}

func TestStartStopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	m := NewManager(&config.Config{
		AgentID:              "agent-a",
		SharedAuthToken:      "secret",
		AgentOrchestratorURL: srv.URL,
		AgentJobPollInterval: time.Millisecond,
	}, log.Noop(), nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}
