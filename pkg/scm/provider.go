package scm

import (
	"context"
	"net/http"
)

//go:generate mockgen -source=provider.go -destination=mock_scm/mock_provider.go -package=mock_scm

type Provider interface {
	Name() string
	ParseWebhook(r *http.Request) ([]Webhook, error)
	GetFile(fileName string, revision string, repo string) (string, error)
	// PullRequestApproved reports whether someone other than the author has
	// an approval that is still in force.
	PullRequestApproved(ctx context.Context, repo string, prNumber int) (bool, error)
}
