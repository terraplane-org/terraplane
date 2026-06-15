package events

type Kind string

const (
	KindPlan    Kind = "plan"
	KindApply   Kind = "apply"
	KindUnlock  Kind = "unlock"
	KindIgnored Kind = "ignored"
	KindUnknown Kind = "unknown"
)

// Event is a sealed domain event produced by an SCM adapter from a webhook payload.
type Event interface {
	isEvent()
	Kind() Kind
}
