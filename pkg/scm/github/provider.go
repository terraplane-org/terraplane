package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
)

const signaturePrefix = "sha256="

type provider struct {
	logger                log.Logger
	github_webhook_secret string
	client                Client
}

func (p *provider) Name() string {
	return "github"
}

func (p *provider) ParseWebhook(r *http.Request) ([]scm.Webhook, error) {
	if _, err := p.verifyWebhookSignature(r); err != nil {
		return nil, err
	}

	github_event := r.Header.Get("X-GitHub-Event")
	switch github_event {
	case "":
		return nil, nil
	case "ping":
		return nil, nil
	case "issue_comment": // This is the main event we're interested in for GH comments
		return p.parseIssueCommentWebhook(r)
	default:
		p.logger.Warn("Received unhandled GitHub event: %s", github_event)
		return nil, nil
	}
}

func (p *provider) GetFile(fileName string, revision string, repo string) (string, error) {
	return p.client.getFile(context.Background(), repo, fileName, revision)
}

func (p *provider) parseIssueCommentWebhook(r *http.Request) ([]scm.Webhook, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var issueCommentWebhook issueCommentWebhook
	err = json.Unmarshal(body, &issueCommentWebhook)
	if err != nil {
		return nil, fmt.Errorf("unmarshal issue comment event: %w", err)
	}

	if issueCommentWebhook.Comment.Body == "" {
		return nil, fmt.Errorf("comment body is empty")
	}

	if issueCommentWebhook.Action != "created" {
		return nil, nil
	}

	if issueCommentWebhook.Issue.PullRequest == nil {
		return nil, nil
	}

	commitSHA, err := p.client.getCommitSHA(context.Background(), issueCommentWebhook.Repository.FullName, issueCommentWebhook.Issue.Number)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit SHA: %w", err)
	}
	scmWebhook := p.webhookToScmWebhook(issueCommentWebhook, commitSHA)

	return []scm.Webhook{scmWebhook}, nil
}

func (p *provider) webhookToScmWebhook(webhook issueCommentWebhook, commitSHA string) scm.Webhook {
	var scmWebhook scm.Webhook
	scmWebhook.RepositorySlug = webhook.Repository.FullName
	scmWebhook.PRNumber = webhook.Issue.Number
	scmWebhook.FullCommand = webhook.Comment.Body
	scmWebhook.TriggeringUser = webhook.Comment.User.Login
	scmWebhook.CommitSHA = commitSHA
	return scmWebhook
}

func (p *provider) verifyWebhookSignature(r *http.Request) ([]byte, error) {
	if p.github_webhook_secret == "" {
		return nil, fmt.Errorf("webhook secret is not configured")
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		return nil, fmt.Errorf("X-Hub-Signature-256 header is missing")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	mac := hmac.New(sha256.New, []byte(p.github_webhook_secret))
	mac.Write(body)
	expected := signaturePrefix + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return nil, fmt.Errorf("webhook signature does not match")
	}

	return body, nil
}

func NewProvider(logger log.Logger, config *config.Config, client Client) scm.Provider {
	return &provider{logger: logger, github_webhook_secret: config.OrchestratorGithubWebhookSecret, client: client}
}
