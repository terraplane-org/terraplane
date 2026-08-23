package scm

import "context"

type CheckState string

const (
	CheckPending CheckState = "pending"
	CheckSuccess CheckState = "success"
	CheckFailure CheckState = "failure"
)

type Check struct {
	Repo        string
	SHA         string
	Key         string
	State       CheckState
	Description string
}

func PlanStackCheckKey(stack string) string {
	return "terraplane/plan: " + stack
}

func PlanRollupCheckKey() string {
	return "terraplane/plan"
}

//go:generate mockgen -source=publisher.go -destination=mock_scm/mock_publisher.go -package=mock_scm

type Publisher interface {
	Name() string
	WriteComment(ctx context.Context, repo string, prNumber int, body string) error
	AcknowledgeComment(ctx context.Context, repo string, prNumber int, commentID int) error
	UpsertCheck(ctx context.Context, check Check) error
}
