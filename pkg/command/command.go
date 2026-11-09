package command

type Kind string

const (
	KindUnknown Kind = "unknown"
	KindPlan    Kind = "plan"
	KindApply   Kind = "apply"
	KindUnlock  Kind = "unlock"
)

const InvalidCommandHelp = "Couldn't parse command.\n\nValid commands:\n\n" +
	"`terraplane plan [-s <stack>]... [-e <environment>]... [-- <terraform flags>]`\n" +
	"`terraplane apply [-s <stack>]... [-e <environment>]...`\n" +
	"`terraplane unlock [-s <stack>]... [-e <environment>]...`\n\n" +
	"Use `-s` or `--stack` to select stacks and `-e` or `--env` to select environments. " +
	"Terraform flags are supported only for `plan` and must follow `--`. " +
	"`unlock` requires at least one stack or environment."

type Command struct {
	Kind       Kind
	ParseError string
	Plan       PlanCommand
	Apply      ApplyCommand
	Unlock     UnlockCommand
}

func (c *Command) JobID() string {
	switch c.Kind {
	case KindPlan:
		return c.Plan.JobID
	case KindApply:
		return c.Apply.JobID
	case KindUnlock:
		return c.Unlock.JobID
	default:
		return ""
	}
}

type base struct {
	Repo, TriggerUser, RawComment, CommitSHA, Agent, JobID, Dir, ToolVersion string
	PRNumber                                                                 int
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
