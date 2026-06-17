package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/scm/commands"
	"github.com/xyzjace/terraplane/pkg/scm/events"
)

const signaturePrefix = "sha256="

type provider struct {
	logger                log.Logger
	github_webhook_secret string
	github_access_token   string

	githubClient Client
}

// CreatePRComment implements scm.Provider.
func (p *provider) CreatePRComment(ctx context.Context, repo string, prNumber int, comment string) error {
	panic("unimplemented")
}

func (p *provider) GetFile(ctx context.Context, repo string, path string) (string, error) {
	file, err := p.githubClient.GetFile(ctx, repo, path)
	if err != nil {
		return "", err
	}
	p.logger.Info("File", "file", file)
	return file, nil
}

func (p *provider) Name() string {
	return "github"
}

func (p *provider) ParseWebhook(ctx context.Context, webhook scm.Webhook) (events.Event, error) {
	if err := p.verifyWebhookSignature(webhook); err != nil {
		return events.Unknown{Reason: err.Error()}, err
	}

	githubEvent := webhook.Headers.Get("X-GitHub-Event")
	switch githubEvent {
	case "":
		return events.Unknown{Reason: "missing X-GitHub-Event header"}, nil
	case "ping":
		return events.Ignored{}, nil
	case "issue_comment":
		return p.parseIssueComment(webhook)
	default:
		p.logger.Warn("Received unhandled GitHub event", "event", githubEvent)
		return events.Unknown{Reason: fmt.Sprintf("unhandled GitHub event: %s", githubEvent)}, nil
	}
}

func (p *provider) parseIssueComment(webhook scm.Webhook) (events.Event, error) {
	var payload issueCommentWebhook
	if err := json.Unmarshal(webhook.Body, &payload); err != nil {
		return events.Unknown{}, fmt.Errorf("unmarshal issue comment webhook: %w", err)
	}

	if payload.Action != "created" {
		return events.Ignored{}, nil
	}

	if payload.Issue.PullRequest == nil {
		return events.Ignored{}, nil
	}

	if payload.Comment.Body == "" {
		return events.Unknown{Reason: "comment body is empty"}, nil
	}

	kind, ok := commands.ParseComment(payload.Comment.Body)
	if !ok {
		return events.Ignored{}, nil
	}

	repo := payload.Repository.FullName
	pr := payload.Issue.Number
	user := payload.Comment.User.Login
	comment := payload.Comment.Body

	switch kind {
	case events.KindPlan:
		p.logger.Info("Received plan event", "repo", repo, "pr", pr)
		return events.Plan{
			RepoSlug:    repo,
			PRNumber:    pr,
			TriggerUser: user,
			RawComment:  comment,
		}, nil
	case events.KindApply:
		p.logger.Info("Received apply event", "repo", repo, "pr", pr)
		return events.Apply{
			RepoSlug:    repo,
			PRNumber:    pr,
			TriggerUser: user,
			RawComment:  comment,
		}, nil
	case events.KindUnlock:
		p.logger.Info("Received unlock event", "repo", repo, "pr", pr)
		return events.Unlock{
			RepoSlug:    repo,
			PRNumber:    pr,
			TriggerUser: user,
			RawComment:  comment,
		}, nil
	default:
		return events.Ignored{}, nil
	}
}

func (p *provider) verifyWebhookSignature(webhook scm.Webhook) error {
	if p.github_webhook_secret == "" {
		return fmt.Errorf("webhook secret is not configured")
	}

	signature := webhook.Headers.Get("X-Hub-Signature-256")
	if signature == "" {
		return fmt.Errorf("X-Hub-Signature-256 header is missing")
	}

	mac := hmac.New(sha256.New, []byte(p.github_webhook_secret))
	mac.Write(webhook.Body)
	expected := signaturePrefix + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("webhook signature does not match")
	}

	return nil
}

func NewProvider(logger log.Logger, config *config.Config, client Client) scm.Provider {
	return &provider{logger: logger, github_webhook_secret: config.OrchestratorGithubWebhookSecret, githubClient: client}
}
