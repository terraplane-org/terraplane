package scm

type Event int

// Event types should have matching parser methods in the Provider interface
const (
	Unknown Event = iota
	Plan
	Apply
	Unlock
	Ignored
)

var eventNames = map[Event]string{
	Plan:    "plan",
	Apply:   "apply",
	Unlock:  "unlock",
	Ignored: "ignored",
	Unknown: "unknown",
}

func (e Event) String() string {
	return eventNames[e]
}
