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
	Stacks       []string
	Environments []string
	PlanFlags    string
}

type ApplyCommand struct {
	base
	Stacks       []string
	Environments []string
}

type UnlockCommand struct {
	base
	Stacks       []string
	Environments []string
}
