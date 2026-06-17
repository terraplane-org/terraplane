package scm

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/scm/events"
)

type Provider interface {
	Name() string
	ParseWebhook(ctx context.Context, webhook Webhook) (events.Event, error)

	GetFile(ctx context.Context, repo string, path string) (string, error)
	CreatePRComment(ctx context.Context, repo string, prNumber int, comment string) error
}
