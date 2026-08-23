package github

import (
	"context"
	"strings"

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

func (p *publisher) AppendNote(ctx context.Context, repo string, prNumber int, body string) error {
	if err := p.client.WriteComment(ctx, repo, prNumber, body); err != nil {
		p.logger.Error("Failed to write PR comment", "repo", repo, "pr", prNumber, "error", err)
		return err
	}
	return nil
}

func (p *publisher) UpsertStatus(ctx context.Context, key scm.StatusKey, body string) error {
	marked := key.Marker() + "\n" + body
	comments, err := p.client.ListIssueComments(ctx, key.Repo, key.PRNumber)
	if err != nil {
		p.logger.Error("Failed to list PR comments for status upsert", "repo", key.Repo, "pr", key.PRNumber, "error", err)
		return err
	}
	for _, comment := range comments {
		if strings.Contains(comment.Body, key.Marker()) {
			if err := p.client.UpdateComment(ctx, key.Repo, int(comment.ID), marked); err != nil {
				p.logger.Error("Failed to update PR status comment", "repo", key.Repo, "pr", key.PRNumber, "comment_id", comment.ID, "error", err)
				return err
			}
			return nil
		}
	}
	return p.AppendNote(ctx, key.Repo, key.PRNumber, marked)
}

func (p *publisher) Name() string {
	return "github"
}

func NewPublisher(logger log.Logger, client Client) scm.Publisher {
	return &publisher{logger: logger, client: client}
}
