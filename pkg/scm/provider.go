package scm

import (
	"net/http"
)

type Provider interface {
	Name() string
	ParseWebhook(r *http.Request) (Event, error)
	// Effectively forcing each implementation of the adapter pattern to implement specific logic for each event variant
	ParsePlanWebhook(r *http.Request) (Event, error)
	ParseApplyWebhook(r *http.Request) (Event, error)
	ParseUnknownWebhook(r *http.Request) (Event, error)
	ParseUnlockWebhook(r *http.Request) (Event, error)
}
