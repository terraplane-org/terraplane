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

func (p *publisher) WriteComment(ctx context.Context, repo string, prNumber int, body string) error {
	if err := p.client.WriteComment(ctx, repo, prNumber, body); err != nil {
		p.logger.Error("Failed to write PR comment", "repo", repo, "pr", prNumber, "error", err)
		return err
	}
	return nil
}

func (p *publisher) UpsertComment(ctx context.Context, repo string, prNumber int, commentID int, body string) (int, error) {
	if commentID != 0 {
		if err := p.client.UpdateComment(ctx, repo, commentID, body); err != nil {
			p.logger.Error("Failed to update PR comment", "repo", repo, "pr", prNumber, "comment_id", commentID, "error", err)
			return 0, err
		}
		return commentID, nil
	}
	id, err := p.client.CreateComment(ctx, repo, prNumber, body)
	if err != nil {
		p.logger.Error("Failed to create PR comment", "repo", repo, "pr", prNumber, "error", err)
		return 0, err
	}
	return id, nil
}

func (p *publisher) Name() string {
	return "github"
}

func NewPublisher(logger log.Logger, client Client) scm.Publisher {
	return &publisher{logger: logger, client: client}
}
