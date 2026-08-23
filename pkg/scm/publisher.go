package scm

import (
	"context"
	"fmt"
)

//go:generate mockgen -source=publisher.go -destination=mock_scm/mock_publisher.go -package=mock_scm

// StatusKey identifies a sticky status note on a change request.
// Kind is the job action (plan, apply, unlock); Stack is the stack name.
type StatusKey struct {
	Repo     string
	PRNumber int
	Stack    string
	Kind     string
}

func (k StatusKey) Marker() string {
	return fmt.Sprintf("<!-- terraplane-status:%s:%s -->", k.Kind, k.Stack)
}

// Note is a change-request comment as returned by an SCM adapter.
type Note struct {
	ID   int64
	Body string
}

type Publisher interface {
	Name() string
	UpsertStatus(ctx context.Context, key StatusKey, body string) error
	AppendNote(ctx context.Context, repo string, prNumber int, body string) error
	AcknowledgeComment(ctx context.Context, repo string, prNumber int, commentID int) error
}
