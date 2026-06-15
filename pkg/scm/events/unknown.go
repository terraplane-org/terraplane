package events

type Unknown struct {
	Reason string
}

func (Unknown) isEvent()   {}
func (Unknown) Kind() Kind { return KindUnknown }
