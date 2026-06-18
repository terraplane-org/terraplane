package scm

import (
	"net/http"
)

type Provider interface {
	Name() string
	ParseWebhook(r *http.Request) ([]Webhook, error)
	GetFile(fileName string, revision string, repo string) (string, error)
}
