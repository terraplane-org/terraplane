package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/xyzjace/terraplane/config"
)

type Client interface {
	getCommitSHA(ctx context.Context, repo string, prNumber int) (string, error)
	getFile(ctx context.Context, repo string, path string, revision string) (string, error)
	writeComment(ctx context.Context, repo string, prNumber int, body string) error
}

type client struct {
	accessToken string
	httpClient  *http.Client
	apiURL      string
}

func (c *client) getCommitSHA(ctx context.Context, repo string, prNumber int) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/pulls/%d", c.apiURL, repo, prNumber)
	var resp struct {
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return "", fmt.Errorf("failed to fetch pull request head commit for repository %s PR #%d: %w", repo, prNumber, err)
	}
	if resp.Head.SHA == "" {
		return "", fmt.Errorf("GitHub returned an empty head commit SHA for repository %s PR #%d", repo, prNumber)
	}
	return resp.Head.SHA, nil
}

func (c *client) getFile(ctx context.Context, repo string, path string, revision string) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/contents/%s", c.apiURL, repo, path)
	if revision != "" {
		var err error
		u, err = appendRefQuery(u, revision)
		if err != nil {
			return "", fmt.Errorf("failed to build GitHub contents URL for repository %s file %q at revision %q: %w", repo, path, revision, err)
		}
	}

	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return "", fmt.Errorf("failed to fetch file %q at revision %q from repository %s: %w", path, revision, repo, err)
	}
	if resp.Encoding != "base64" {
		return "", fmt.Errorf("GitHub returned unsupported file encoding %q for repository %s file %q at revision %q", resp.Encoding, repo, path, revision)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 file content for repository %s file %q at revision %q: %w", repo, path, revision, err)
	}
	return string(decoded), nil
}

func (c *client) getJSON(ctx context.Context, u string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute GitHub API request to %s: %w", u, err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read GitHub API response body from %s: %w", u, err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API request to %s returned unexpected status %s: %s", u, res.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode GitHub API response from %s: %w", u, err)
	}
	return nil
}

func (c *client) newRequest(ctx context.Context, method, u string, body io.Reader) (*http.Request, error) {
	if c.accessToken == "" {
		return nil, fmt.Errorf("GitHub access token is not configured. Please set ORCHESTRATOR_GITHUB_ACCESS_TOKEN in the environment")
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub API request to %s: %w", u, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *client) writeComment(ctx context.Context, repo string, prNumber int, body string) error {
	u := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.apiURL, repo, prNumber)
	payloadBytes, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("failed to marshal comment payload for repository %s PR #%d: %w", repo, prNumber, err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, u, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute GitHub API request to write comment to repository %s PR #%d: %w", repo, prNumber, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("GitHub API request to write comment to repository %s PR #%d returned unexpected status %s: %s", repo, prNumber, res.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func appendRefQuery(u, ref string) (string, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return "", fmt.Errorf("failed to parse GitHub contents URL %q: %w", u, err)
	}
	q := parsed.Query()
	q.Set("ref", ref)
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func NewClient(config *config.Config) Client {
	return &client{
		accessToken: config.OrchestratorGithubAccessToken,
		httpClient:  &http.Client{},
		apiURL:      "https://api.github.com",
	}
}
