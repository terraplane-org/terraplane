package scm

type Event int

// Event types should have matching parser methods in the Provider interface
const (
	Unknown Event = iota
	Plan
	Apply
	Unlock
)

var eventNames = map[Event]string{
	Plan:  "plan",
	Apply: "apply",
}

func (e Event) String() string {
	return eventNames[e]
}
