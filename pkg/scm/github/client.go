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
	"time"
	"unicode/utf8"

	"github.com/xyzjace/terraplane/config"
)

//go:generate mockgen -source=client.go -destination=mock_github/mock_client.go -package=mock_github

type Client interface {
	GetCommitSHA(ctx context.Context, repo string, prNumber int) (string, error)
	GetFile(ctx context.Context, repo string, path string, revision string) (string, error)
	WriteComment(ctx context.Context, repo string, prNumber int, body string) (int, error)
	UpdateComment(ctx context.Context, repo string, prNumber int, body string, commentID int) error
	ReactToComment(ctx context.Context, repo string, commentID int, reaction string) error
	SetCommitStatus(ctx context.Context, repo, sha, state, contextName, description string) error
	PullRequestApproved(ctx context.Context, repo string, prNumber int) (bool, error)
}

type client struct {
	accessToken string
	httpClient  *http.Client
	apiURL      string
}

func (c *client) ReactToComment(ctx context.Context, repo string, commentID int, reaction string) error {
	u := fmt.Sprintf("%s/repos/%s/issues/comments/%d/reactions", c.apiURL, repo, commentID)
	payloadBytes, err := json.Marshal(map[string]string{"content": reaction})
	if err != nil {
		return fmt.Errorf("failed to marshal reaction payload for repository %s comment #%d: %w", repo, commentID, err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, u, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute GitHub API request to react to comment in repository %s comment #%d: %w", repo, commentID, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("GitHub API request to react to comment in repository %s comment #%d returned unexpected status %s: %s", repo, commentID, res.Status, strings.TrimSpace(string(respBody)))
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (c *client) GetCommitSHA(ctx context.Context, repo string, prNumber int) (string, error) {
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

func (c *client) GetFile(ctx context.Context, repo string, path string, revision string) (string, error) {
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

func (c *client) WriteComment(ctx context.Context, repo string, prNumber int, body string) (int, error) {
	u := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.apiURL, repo, prNumber)
	payloadBytes, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal comment payload for repository %s PR #%d: %w", repo, prNumber, err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, u, bytes.NewReader(payloadBytes))
	if err != nil {
		return 0, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to execute GitHub API request to write comment to repository %s PR #%d: %w", repo, prNumber, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(res.Body)
		return 0, fmt.Errorf("GitHub API request to write comment to repository %s PR #%d returned unexpected status %s: %s", repo, prNumber, res.Status, strings.TrimSpace(string(respBody)))
	}
	respBody, _ := io.ReadAll(res.Body)
	var respData commentCreatedResponse
	if err := json.Unmarshal(respBody, &respData); err != nil {
		return 0, fmt.Errorf("failed to decode GitHub API response for write comment: %w", err)
	}
	return respData.ID, nil
}

func (c *client) UpdateComment(ctx context.Context, repo string, prNumber int, body string, commentID int) error {
	u := fmt.Sprintf("%s/repos/%s/issues/comments/%d", c.apiURL, repo, commentID)
	payloadBytes, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("failed to marshal comment payload for repository %s PR #%d: %w", repo, prNumber, err)
	}

	req, err := c.newRequest(ctx, http.MethodPatch, u, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute GitHub API request to update comment in repository %s PR #%d: %w", repo, prNumber, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("GitHub API request to update comment in repository %s PR #%d returned unexpected status %s: %s", repo, prNumber, res.Status, strings.TrimSpace(string(respBody)))
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (c *client) SetCommitStatus(ctx context.Context, repo, sha, state, contextName, description string) error {
	description = truncateRunes(description, 140)
	u := fmt.Sprintf("%s/repos/%s/statuses/%s", c.apiURL, repo, sha)
	payloadBytes, err := json.Marshal(map[string]string{
		"state":       state,
		"context":     contextName,
		"description": description,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal commit status for repository %s sha %s: %w", repo, sha, err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, u, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute GitHub API request to set commit status for repository %s sha %s: %w", repo, sha, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("GitHub API request to set commit status for repository %s sha %s returned unexpected status %s: %s", repo, sha, res.Status, strings.TrimSpace(string(respBody)))
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (c *client) PullRequestApproved(ctx context.Context, repo string, prNumber int) (bool, error) {
	author, err := c.pullRequestAuthor(ctx, repo, prNumber)
	if err != nil {
		return false, err
	}
	reviews, err := c.listPullRequestReviews(ctx, repo, prNumber)
	if err != nil {
		return false, err
	}
	return approvedByOther(author, reviews), nil
}

func (c *client) pullRequestAuthor(ctx context.Context, repo string, prNumber int) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/pulls/%d", c.apiURL, repo, prNumber)
	var resp struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return "", fmt.Errorf("failed to fetch pull request author for repository %s PR #%d: %w", repo, prNumber, err)
	}
	if strings.TrimSpace(resp.User.Login) == "" {
		return "", fmt.Errorf("GitHub returned an empty author for repository %s PR #%d", repo, prNumber)
	}
	return resp.User.Login, nil
}

func (c *client) listPullRequestReviews(ctx context.Context, repo string, prNumber int) ([]pullReview, error) {
	var all []pullReview
	for page := 1; page <= 20; page++ {
		u := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews?per_page=100&page=%d", c.apiURL, repo, prNumber, page)
		var batch []pullReview
		if err := c.getJSON(ctx, u, &batch); err != nil {
			return nil, fmt.Errorf("failed to list pull request reviews for repository %s PR #%d: %w", repo, prNumber, err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

type pullReview struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// approvedByOther reports whether a reviewer other than the author still has
// an approval. The latest APPROVED, CHANGES_REQUESTED, or DISMISSED review
// from each reviewer is the one that counts. Comments do not change it.
func approvedByOther(author string, reviews []pullReview) bool {
	type decision struct {
		at     time.Time
		index  int
		status string
	}
	latest := make(map[string]decision)
	for i, review := range reviews {
		login := strings.TrimSpace(review.User.Login)
		if login == "" || strings.EqualFold(login, author) {
			continue
		}
		var status string
		switch review.State {
		case "APPROVED":
			status = "approved"
		case "CHANGES_REQUESTED":
			status = "rejected"
		case "DISMISSED":
			status = "dismissed"
		default:
			continue
		}
		key := strings.ToLower(login)
		prev, ok := latest[key]
		if ok && (review.SubmittedAt.Before(prev.at) || (review.SubmittedAt.Equal(prev.at) && i < prev.index)) {
			continue
		}
		latest[key] = decision{at: review.SubmittedAt, index: i, status: status}
	}
	for _, item := range latest {
		if item.status == "approved" {
			return true
		}
	}
	return false
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
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
