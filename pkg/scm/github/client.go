package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xyzjace/terraplane/config"
)

type Client interface {
	GetFile(ctx context.Context, repo string, path string) (string, error)
}

type client struct {
	accessToken string
	httpClient  *http.Client
	apiURL      string
}

func (c *client) GetFile(ctx context.Context, repo string, path string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.apiURL, repo, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))
	return "", nil
}

func NewClient(config *config.Config) Client {
	return &client{
		accessToken: config.OrchestratorGithubAccessToken,
		httpClient:  &http.Client{},
		apiURL:      "https://api.github.com",
	}
}
