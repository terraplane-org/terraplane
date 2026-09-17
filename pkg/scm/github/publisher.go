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

func (p *publisher) AcknowledgeComment(ctx context.Context, repo string, prNumber int, commentID int) error {
	if err := p.client.ReactToComment(ctx, repo, commentID, "+1"); err != nil {
		p.logger.Error("Failed to react to PR comment", "repo", repo, "pr", prNumber, "error", err)
		return err
	}
	return nil
}

func (p *publisher) WriteComment(ctx context.Context, repo string, prNumber int, body string) (int, error) {
	commentID, err := p.client.WriteComment(ctx, repo, prNumber, body)
	if err != nil {
		p.logger.Error("Failed to write PR comment", "repo", repo, "pr", prNumber, "error", err)
		return 0, err
	}
	return commentID, nil
}

func (p *publisher) UpdateComment(ctx context.Context, repo string, prNumber int, body string, commentID int) error {
	if err := p.client.UpdateComment(ctx, repo, prNumber, body, commentID); err != nil {
		p.logger.Error("Failed to update PR comment", "repo", repo, "pr", prNumber, "error", err)
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
