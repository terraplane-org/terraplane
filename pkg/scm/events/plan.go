package events

type Plan struct {
	RepoSlug    string
	PRNumber    int
	TriggerUser string
	CommitSHA   string
	RawComment  string
}

func (Plan) isEvent()   {}
func (Plan) Kind() Kind { return KindPlan }
