package github

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type publisher struct {
	logger log.Logger
	client Client
}

// WriteComment implements scm.Publisher.
func (p *publisher) WriteComment(ctx context.Context, repo string, prNumber int, body string) error {
	if err := p.client.WriteComment(ctx, repo, prNumber, body); err != nil {
		p.logger.Error("Failed to write PR comment", "repo", repo, "pr", prNumber, "error", err)
		return err
	}
	return nil
}

func (p *publisher) Name() string {
	return "github"
}

func NewPublisher(logger log.Logger, client Client) scm.Publisher {
	return &publisher{logger: logger, client: client}
}
