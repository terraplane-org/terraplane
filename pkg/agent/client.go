package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/storage/models"
)

type claimedJob struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Repo      string `json:"repo"`
	PRNumber  int32  `json:"pr_number"`
	StackName string `json:"stack_name"`
	Dir       string `json:"dir"`
	CommitSHA string `json:"commit_sha"`
	PlanFlags string `json:"plan_flags,omitempty"`
}

type jobResultBody struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

type orchestratorClient struct {
	baseURL    string
	agentID    string
	token      string
	httpClient *http.Client
	claimWait  time.Duration
}

func newOrchestratorClient(baseURL, agentID, token string, claimWait time.Duration) *orchestratorClient {
	return &orchestratorClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		agentID:    agentID,
		token:      token,
		httpClient: &http.Client{},
		claimWait:  claimWait,
	}
}

func (c *orchestratorClient) Claim(ctx context.Context) (*claimedJob, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/agents/" + url.PathEscape(c.agentID) + "/jobs/claim")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("wait", c.claimWait.String())
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth.BearerHeader(c.token))

	// Allow the long-poll to run slightly longer than the server wait.
	client := *c.httpClient
	client.Timeout = c.claimWait + 10*time.Second

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var job claimedJob
		if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
			return nil, fmt.Errorf("decode claimed job: %w", err)
		}
		return &job, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("claim failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
}

func (c *orchestratorClient) ReportResult(ctx context.Context, jobID string, success bool, output, errMsg string) error {
	u := c.baseURL + "/api/v1/agents/" + url.PathEscape(c.agentID) + "/jobs/" + url.PathEscape(jobID) + "/result"
	payload, err := json.Marshal(jobResultBody{Success: success, Output: output, Error: errMsg})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth.BearerHeader(c.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("report result failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (j *claimedJob) action() models.JobAction {
	return models.JobAction(j.Action)
}
