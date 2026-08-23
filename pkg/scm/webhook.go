package scm

type EventKind string

const (
	EventKindCommand       EventKind = "command"
	EventKindChangeUpdated EventKind = "change_updated"
)

type Webhook struct {
	Kind           EventKind
	RepositorySlug string
	PRNumber       int
	FullCommand    string
	TriggeringUser string
	CommitSHA      string
	CommentID      int
	ExtraData      map[string]any
}
