package scm

import "context"

//go:generate mockgen -source=publisher.go -destination=mock_scm/mock_publisher.go -package=mock_scm

type Publisher interface {
	Name() string
	WriteComment(ctx context.Context, repo string, prNumber int, body string) error
	AcknowledgeComment(ctx context.Context, repo string, prNumber int, commentID int) error
	// UpsertComment creates a PR comment when commentID is 0, otherwise edits it.
	// Returns the comment ID GitHub assigned.
	UpsertComment(ctx context.Context, repo string, prNumber int, commentID int, body string) (int, error)
}
