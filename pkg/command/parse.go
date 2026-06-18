package command

import (
	"strings"

	"github.com/xyzjace/terraplane/pkg/scm"
)

func ParseWebhook(w *scm.Webhook) Command {
	b := base{
		Repo:        w.RepositorySlug,
		PRNumber:    w.PRNumber,
		TriggerUser: w.TriggeringUser,
		RawComment:  w.FullCommand,
		CommitSHA:   w.CommitSHA,
	}
	switch verb(w.FullCommand) {
	case KindPlan:
		return Command{Kind: KindPlan, Plan: PlanCommand{base: b, Stacks: stacks(w.FullCommand)}}
	case KindApply:
		return Command{Kind: KindApply, Apply: ApplyCommand{base: b}}
	case KindUnlock:
		return Command{Kind: KindUnlock, Unlock: UnlockCommand{base: b}}
	default:
		return Command{Kind: KindUnknown}
	}
}

func verb(body string) Kind {
	fields := strings.Fields(firstLine(body))
	if len(fields) < 2 || !strings.EqualFold(fields[0], "terraplane") {
		return KindUnknown
	}
	switch strings.ToLower(fields[1]) {
	case "plan":
		return KindPlan
	case "apply":
		return KindApply
	case "unlock":
		return KindUnlock
	default:
		return KindUnknown
	}
}

func stacks(body string) []string {
	fields := strings.Fields(firstLine(body))
	start := 0
	if len(fields) >= 2 && strings.EqualFold(fields[0], "terraplane") && strings.EqualFold(fields[1], "plan") {
		start = 2
	}

	var out []string
	for i := start; i < len(fields); i++ {
		switch fields[i] {
		case "-s", "-stack":
			i++
			if i < len(fields) {
				out = append(out, fields[i])
			}
		default:
			if name, ok := strings.CutPrefix(fields[i], "-s="); ok {
				out = append(out, name)
			} else if name, ok := strings.CutPrefix(fields[i], "-stack="); ok {
				out = append(out, name)
			}
		}
	}
	return out
}

func firstLine(body string) string {
	line := strings.TrimSpace(body)
	if i := strings.Index(line, "\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}
