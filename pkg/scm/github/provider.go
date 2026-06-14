package github

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
}

func (p *provider) Name() string {
	return "github"
}

func (p *provider) ParseWebhook(r *http.Request) (scm.Event, error) {
	if _, err := p.verifyWebhookSignature(r); err != nil {
		return scm.Unknown, err
	}

	// TODO: This needs to handle the comment webhook parsing first to determine what kind of action to take
	github_event := r.Header.Get("X-GitHub-Event")
	switch github_event {
	case "":
		return p.ParseUnknownWebhook(r)
	case "plan":
		return p.ParsePlanWebhook(r)
	case "apply":
		return p.ParseApplyWebhook(r)
	case "unlock":
		return p.ParseUnlockWebhook(r)
	}
	return scm.Unknown, fmt.Errorf("unhandled GitHub event: %s", github_event)
}

func (p *provider) ParsePlanWebhook(r *http.Request) (scm.Event, error) {
	p.logger.Info("Received plan webhook!")
	return scm.Plan, nil
}

func (p *provider) ParseApplyWebhook(r *http.Request) (scm.Event, error) {
	return scm.Apply, nil
}

func (p *provider) ParseUnknownWebhook(r *http.Request) (scm.Event, error) {
	return scm.Unknown, nil
}

func (p *provider) ParseUnlockWebhook(r *http.Request) (scm.Event, error) {
	return scm.Unlock, nil
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
