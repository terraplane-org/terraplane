package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type Client interface {
	GetPullRequest(ctx context.Context, repo string, prNumber int) (scm.PullRequest, error)
	GetFile(ctx context.Context, repo string, path string, ref string) (string, error)
}

type client struct {
	accessToken string
	httpClient  *http.Client
	apiURL      string
}

func (c *client) GetPullRequest(ctx context.Context, repo string, prNumber int) (scm.PullRequest, error) {
	u := fmt.Sprintf("%s/repos/%s/pulls/%d", c.apiURL, repo, prNumber)
	var resp struct {
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return scm.PullRequest{}, err
	}
	return scm.PullRequest{HeadSHA: resp.Head.SHA, HeadRef: resp.Head.Ref}, nil
}

func (c *client) GetFile(ctx context.Context, repo string, path string, ref string) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/contents/%s", c.apiURL, repo, path)
	if ref != "" {
		var err error
		u, err = appendRefQuery(u, ref)
		if err != nil {
			return "", err
		}
	}

	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return "", err
	}
	if resp.Encoding != "base64" {
		return "", fmt.Errorf("unsupported content encoding %q", resp.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("decode file content: %w", err)
	}
	return string(decoded), nil
}

func (c *client) getJSON(ctx context.Context, u string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github api %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode github response: %w", err)
	}
	return nil
}

func (c *client) newRequest(ctx context.Context, method, u string) (*http.Request, error) {
	if c.accessToken == "" {
		return nil, fmt.Errorf("github access token is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

func appendRefQuery(u, ref string) (string, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
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
