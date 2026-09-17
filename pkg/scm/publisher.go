package scm

import "context"

//go:generate mockgen -source=publisher.go -destination=mock_scm/mock_publisher.go -package=mock_scm

type Publisher interface {
	Name() string
	WriteComment(ctx context.Context, repo string, prNumber int, body string) (int, error)
	AcknowledgeComment(ctx context.Context, repo string, prNumber int, commentID int) error
	UpdateComment(ctx context.Context, repo string, prNumber int, body string, commentID int) error
}
