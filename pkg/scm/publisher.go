package scm

import "context"

type Publisher interface {
	Name() string
	WriteComment(ctx context.Context, repo string, prNumber int, body string) error
}
