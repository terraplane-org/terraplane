package scm

type Webhook struct {
	RepositorySlug string
	PRNumber       int
	FullCommand    string
	TriggeringUser string
	CommitSHA      string
	CommentID      int
	ExtraData      map[string]any
}
