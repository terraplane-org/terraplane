package scm

type Webhook struct {
	RepositorySlug string
	PRNumber       int
	FullCommand    string
	TriggeringUser string
	ExtraData      map[string]any
}
