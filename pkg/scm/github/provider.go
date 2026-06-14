package github

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"

	"github.com/xyzjace/terraplane/pkg/scm"
)

const signaturePrefix = "sha256="

type provider struct {
	logger                log.Logger
	github_webhook_secret string
}

func (p *provider) Name() string {
	return "github"
}

func (p *provider) ParseWebhook(r *http.Request) (scm.Event, error) {
	if _, err := p.verifyWebhookSignature(r); err != nil {
		return scm.Unknown, err
	}

	github_event := r.Header.Get("X-GitHub-Event")
	switch github_event {
	case "":
		return p.ParseUnknownWebhook(r)
	case "ping":
		return p.ParseIgnoredEvent(r)
	case "issue_comment": // This is the main event we're interested in for GH comments
		return p.parseIssueCommentWebhook(r)
	default:
		p.logger.Warn("Received unhandled GitHub event: %s", github_event)
		return scm.Unknown, nil
	}
}

func (p *provider) ParseIgnoredEvent(r *http.Request) (scm.Event, error) {
	return scm.Ignored, nil
}

func (p *provider) ParsePlanWebhook(r *http.Request) (scm.Event, error) {
	p.logger.Info("Received plan webhook!")
	return scm.Plan, nil
}

func (p *provider) ParseApplyWebhook(r *http.Request) (scm.Event, error) {
	p.logger.Info("Received apply webhook!")
	return scm.Apply, nil
}

func (p *provider) ParseUnknownWebhook(r *http.Request) (scm.Event, error) {
	return scm.Unknown, nil
}

func (p *provider) ParseUnlockWebhook(r *http.Request) (scm.Event, error) {
	p.logger.Info("Received unlock webhook!")
	return scm.Unlock, nil
}

func (p *provider) parseIssueCommentWebhook(r *http.Request) (scm.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return scm.Unknown, fmt.Errorf("read request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var event issueCommentWebhook
	err = json.Unmarshal(body, &event)
	if err != nil {
		return scm.Unknown, fmt.Errorf("unmarshal issue comment event: %w", err)
	}

	if event.Comment.Body == "" {
		return scm.Unknown, fmt.Errorf("comment body is empty")
	}

	if event.Action != "created" {
		return scm.Ignored, nil
	}

	// TODO: This will require auth checks to ensure the comment author has permissions to trigger actions
	// TODO: We may want to support configurable prefixes for commands, e.g. "tp plan" instead of "terraplane plan"
	if strings.Contains(event.Comment.Body, "terraplane plan") {
		return p.ParsePlanWebhook(r)
	}
	if strings.Contains(event.Comment.Body, "terraplane apply") {
		return p.ParseApplyWebhook(r)
	}
	if strings.Contains(event.Comment.Body, "terraplane unlock") {
		return p.ParseUnlockWebhook(r)
	}

	return scm.Unknown, nil
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

func NewProvider(logger log.Logger, config *config.Config) scm.Provider {
	return &provider{logger: logger, github_webhook_secret: config.OrchestratorGithubWebhookSecret}
}
