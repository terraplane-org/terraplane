package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/pkg/agentapi"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

func TestNormalizeOrchestratorURL(t *testing.T) {
	require.Equal(t, "http://orch:8080", normalizeOrchestratorURL("ws://orch:8080/ws"))
	require.Equal(t, "https://orch.example.com", normalizeOrchestratorURL("wss://orch.example.com/ws/"))
	require.Equal(t, "http://orch:8080", normalizeOrchestratorURL("http://orch:8080/"))
	require.Equal(t, "https://orch.example.com", normalizeOrchestratorURL("https://orch.example.com"))
}

func TestOrchClientPollAckHeartbeatResult(t *testing.T) {
	var gotPath, gotMethod, gotAgent, gotAuth string
	var resultBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotAgent = r.Header.Get(agentapi.AgentIDHeader)
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/agent/jobs/poll":
			if r.URL.RawQuery == "empty" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentapi.Job{JobID: "job-1", Action: "plan"})
		case "/agent/jobs/job-1/ack", "/agent/jobs/job-1/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case "/agent/jobs/job-1/result":
			resultBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)

	c := newOrchClient(srv.URL, "agent-a", "secret")

	job, err := c.Poll(context.Background())
	require.NoError(t, err)
	require.Equal(t, "job-1", job.JobID)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "agent-a", gotAgent)
	require.Equal(t, "Bearer secret", gotAuth)

	require.NoError(t, c.Ack(context.Background(), "job-1"))
	require.Equal(t, "/agent/jobs/job-1/ack", gotPath)
	require.NoError(t, c.Heartbeat(context.Background(), "job-1"))
	require.Equal(t, "/agent/jobs/job-1/heartbeat", gotPath)
	require.NoError(t, c.Result(context.Background(), "job-1", agentapi.Result{Success: true, Output: "ok"}))
	require.Contains(t, string(resultBody), `"success":true`)
}

func TestOrchClientPollNoContentAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/jobs/poll":
			w.WriteHeader(http.StatusNoContent)
		case "/agent/jobs/job-1/ack":
			w.WriteHeader(http.StatusInternalServerError)
		case "/agent/jobs/job-1/result":
			w.WriteHeader(http.StatusBadRequest)
		case "/agent/jobs/bad-json/poll-not-used":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "nope")
		}
	}))
	t.Cleanup(srv.Close)

	c := newOrchClient(srv.URL, "agent-a", "secret")
	job, err := c.Poll(context.Background())
	require.NoError(t, err)
	require.Nil(t, job)

	err = c.Ack(context.Background(), "job-1")
	require.Error(t, err)
	err = c.Result(context.Background(), "job-1", agentapi.Result{Success: false})
	require.Error(t, err)
}

func TestOrchClientPollBadStatusAndJSON(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{`)
	}))
	t.Cleanup(srv.Close)

	c := newOrchClient(srv.URL, "agent-a", "secret")
	_, err := c.Poll(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "poll:")
	_, err = c.Poll(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode poll")
}

func TestOrchClientWriteRoutes(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	c := newOrchClient(srv.URL, "agent-a", "secret")
	ctx := context.Background()

	require.NoError(t, c.Write(ctx, &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Ack{Ack: &terraplanev1.Ack{Message: "ok"}},
	}))
	require.NoError(t, c.Write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true, Output: "plan"},
		},
	}))
	require.NoError(t, c.Write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: false, Error: "boom"},
		},
	}))
	require.NoError(t, c.Write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_UnlockResult{
			UnlockResult: &terraplanev1.UnlockResult{Success: true},
		},
	}))
	require.Equal(t, []string{
		"/agent/jobs/job-1/ack",
		"/agent/jobs/job-1/result",
		"/agent/jobs/job-1/result",
	}, paths)
}
