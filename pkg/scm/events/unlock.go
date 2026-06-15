package events

type Unlock struct {
	RepoSlug    string
	PRNumber    int
	TriggerUser string
	CommitSHA   string
	RawComment  string
}

func (Unlock) isEvent()   {}
func (Unlock) Kind() Kind { return KindUnlock }
