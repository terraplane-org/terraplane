package command

import (
	"strings"

	"github.com/xyzjace/terraplane/pkg/scm"
)

func ParseWebhook(w *scm.Webhook) Command {
	kind := verb(w.FullCommand)
	if kind == KindUnknown || !validArgs(kind, w.FullCommand) {
		return Command{Kind: KindUnknown}
	}

	b := base{
		Repo:        w.RepositorySlug,
		PRNumber:    w.PRNumber,
		TriggerUser: w.TriggeringUser,
		RawComment:  w.FullCommand,
		CommitSHA:   w.CommitSHA,
	}
	switch kind {
	case KindPlan:
		return Command{Kind: KindPlan, Plan: PlanCommand{
			base:         b,
			Stacks:       stacks(w.FullCommand),
			Environments: environments(w.FullCommand),
			PlanFlags:    planFlags(w.FullCommand),
		}}
	case KindApply:
		return Command{Kind: KindApply, Apply: ApplyCommand{
			base:         b,
			Stacks:       stacks(w.FullCommand),
			Environments: environments(w.FullCommand),
		}}
	}
	return Command{Kind: KindUnlock, Unlock: UnlockCommand{
		base:         b,
		Stacks:       stacks(w.FullCommand),
		Environments: environments(w.FullCommand),
	}}
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

// validArgs rejects positional arguments and incomplete flags.
//
// Valid forms:
//
//	terraplane plan|apply|unlock [-s NAME|-stack NAME|-e NAME|-env NAME]...
//	terraplane plan ... [--] <terraform plan flags>
//	(Terraform flags that require a separate value must use -flag=value or come after "--".)
//
// Bare words like "terraplane plan stackname" are invalid.
func validArgs(kind Kind, body string) bool {
	fields := strings.Fields(firstLine(body))
	// verb() already requires len(fields) >= 2 before calling validArgs.
	for i := 2; i < len(fields); i++ {
		tok := fields[i]

		if kind == KindPlan && tok == "--" {
			return true
		}

		switch tok {
		case "-s", "-stack", "-e", "-env":
			i++
			if i >= len(fields) || strings.HasPrefix(fields[i], "-") {
				return false
			}
			continue
		}

		if strings.HasPrefix(tok, "-s=") ||
			strings.HasPrefix(tok, "-stack=") ||
			strings.HasPrefix(tok, "-e=") ||
			strings.HasPrefix(tok, "-env=") {
			continue
		}

		// Positional args are never valid.
		if !strings.HasPrefix(tok, "-") {
			return false
		}

		// Apply/unlock only accept terraplane flags; plan also accepts terraform flags.
		if kind != KindPlan {
			return false
		}
	}
	return true
}

func stacks(body string) []string {
	return flagValues(body, "-s", "-stack")
}

func environments(body string) []string {
	return flagValues(body, "-e", "-env")
}

func flagValues(body string, short, long string) []string {
	fields := strings.Fields(firstLine(body))
	start := 0
	if len(fields) >= 2 && strings.EqualFold(fields[0], "terraplane") && isVerb(fields[1]) {
		start = 2
	}

	var out []string
	for i := start; i < len(fields); i++ {
		switch fields[i] {
		case short, long:
			i++
			if i < len(fields) && !strings.HasPrefix(fields[i], "-") {
				out = append(out, fields[i])
			}
		default:
			if name, ok := strings.CutPrefix(fields[i], short+"="); ok {
				out = append(out, name)
			} else if name, ok := strings.CutPrefix(fields[i], long+"="); ok {
				out = append(out, name)
			}
		}
	}
	return out
}

func isVerb(s string) bool {
	switch strings.ToLower(s) {
	case "plan", "apply", "unlock":
		return true
	default:
		return false
	}
}

func planFlags(body string) string {
	fields := strings.Fields(firstLine(body))
	start := 0
	if len(fields) >= 2 && strings.EqualFold(fields[0], "terraplane") && strings.EqualFold(fields[1], "plan") {
		start = 2
	}

	var flags []string
	for i := start; i < len(fields); i++ {
		switch fields[i] {
		case "-s", "-stack", "-e", "-env":
			i++
		case "--":
			return strings.Join(fields[i+1:], " ")
		default:
			if strings.HasPrefix(fields[i], "-s=") ||
				strings.HasPrefix(fields[i], "-stack=") ||
				strings.HasPrefix(fields[i], "-e=") ||
				strings.HasPrefix(fields[i], "-env=") {
				continue
			}
			flags = append(flags, fields[i])
		}
	}
	return strings.Join(flags, " ")
}

func firstLine(body string) string {
	line := strings.TrimSpace(body)
	if i := strings.Index(line, "\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}
