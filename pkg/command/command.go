package command

type Kind string

const (
	KindUnknown Kind = "unknown"
	KindPlan    Kind = "plan"
	KindApply   Kind = "apply"
	KindUnlock  Kind = "unlock"
)

type Command struct {
	Kind   Kind
	Plan   PlanCommand
	Apply  ApplyCommand
	Unlock UnlockCommand
}

type base struct {
	Repo, TriggerUser, RawComment, CommitSHA string
	PRNumber                                 int
}

type PlanCommand struct {
	base
	Stacks []string
}

type ApplyCommand struct{ base }

type UnlockCommand struct{ base }
