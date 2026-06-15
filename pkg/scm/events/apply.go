package events

type Apply struct {
	RepoSlug    string
	PRNumber    int
	TriggerUser string
	CommitSHA   string
	RawComment  string
}

func (Apply) isEvent()   {}
func (Apply) Kind() Kind { return KindApply }
