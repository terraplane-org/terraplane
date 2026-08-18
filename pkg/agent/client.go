package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/agentapi"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type orchClient struct {
	baseURL    string
	agentID    string
	token      string
	httpClient *http.Client
}

func newOrchClient(rawURL, agentID, token string) *orchClient {
	return &orchClient{
		baseURL:    normalizeOrchestratorURL(rawURL),
		agentID:    agentID,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func normalizeOrchestratorURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	u = strings.TrimSuffix(u, "/ws")
	switch {
	case strings.HasPrefix(u, "ws://"):
		u = "http://" + strings.TrimPrefix(u, "ws://")
	case strings.HasPrefix(u, "wss://"):
		u = "https://" + strings.TrimPrefix(u, "wss://")
	}
	return strings.TrimRight(u, "/")
}

func (c *orchClient) Poll(ctx context.Context) (*agentapi.Job, error) {
	resp, err := c.do(ctx, http.MethodPost, "/agent/jobs/poll", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poll: %s", resp.Status)
	}
	var job agentapi.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("decode poll: %w", err)
	}
	return &job, nil
}

func (c *orchClient) Ack(ctx context.Context, jobID string) error {
	return c.empty(ctx, http.MethodPost, "/agent/jobs/"+jobID+"/ack")
}

func (c *orchClient) Heartbeat(ctx context.Context, jobID string) error {
	return c.empty(ctx, http.MethodPost, "/agent/jobs/"+jobID+"/heartbeat")
}

func (c *orchClient) Result(ctx context.Context, jobID string, result agentapi.Result) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, "/agent/jobs/"+jobID+"/result", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("result: %s", resp.Status)
	}
	return nil
}

func (c *orchClient) Write(ctx context.Context, env *terraplanev1.TerraformEnvelope) error {
	switch {
	case env.GetAck() != nil:
		return c.Ack(ctx, env.GetJobId())
	case env.GetPlanResult() != nil:
		r := env.GetPlanResult()
		return c.Result(ctx, env.GetJobId(), agentapi.Result{Success: r.GetSuccess(), Output: r.GetOutput(), Error: r.GetError()})
	case env.GetApplyResult() != nil:
		r := env.GetApplyResult()
		return c.Result(ctx, env.GetJobId(), agentapi.Result{Success: r.GetSuccess(), Output: r.GetOutput(), Error: r.GetError()})
	default:
		return nil
	}
}

func (c *orchClient) empty(ctx context.Context, method, path string) error {
	resp, err := c.do(ctx, method, path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	return nil
}

func (c *orchClient) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth.BearerHeader(c.token))
	req.Header.Set(agentapi.AgentIDHeader, c.agentID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}
