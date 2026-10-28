package scm

import "context"

type CheckState string

const (
	CheckPending CheckState = "pending"
	CheckSuccess CheckState = "success"
	CheckFailure CheckState = "failure"
)

// Check is a commit status. Key is the status name the git host shows,
// for example terraplane/plan.
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
	WriteComment(ctx context.Context, repo string, prNumber int, body string) (int, error)
	AcknowledgeComment(ctx context.Context, repo string, prNumber int, commentID int) error
	UpdateComment(ctx context.Context, repo string, prNumber int, body string, commentID int) error
	UpsertCheck(ctx context.Context, check Check) error
}
