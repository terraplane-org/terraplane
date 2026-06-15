package events

type Ignored struct{}

func (Ignored) isEvent() {}
func (Ignored) Kind() Kind { return KindIgnored }
