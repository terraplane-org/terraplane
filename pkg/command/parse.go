package command

import (
	"io"
	"strings"

	"github.com/spf13/pflag"

	"github.com/xyzjace/terraplane/pkg/scm"
)

func ParseWebhook(w *scm.Webhook) Command {
	fields := strings.Fields(firstLine(w.FullCommand))
	kind := verb(fields)
	if kind == KindUnknown {
		return Command{Kind: KindUnknown}
	}

	stacks, envs, rest, ok := parseArgs(fields[2:])
	if !ok {
		return Command{Kind: KindUnknown}
	}
	if kind != KindPlan && len(rest) > 0 {
		return Command{Kind: KindUnknown}
	}
	if kind == KindPlan && len(rest) > 0 && !hasDoubleDash(fields[2:]) {
		return Command{Kind: KindUnknown}
	}
	if kind == KindUnlock && len(stacks) == 0 && len(envs) == 0 {
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
			Stacks:       stacks,
			Environments: envs,
			PlanFlags:    strings.Join(rest, " "),
		}}
	case KindApply:
		return Command{Kind: KindApply, Apply: ApplyCommand{
			base:         b,
			Stacks:       stacks,
			Environments: envs,
		}}
	}
	return Command{Kind: KindUnlock, Unlock: UnlockCommand{
		base:         b,
		Stacks:       stacks,
		Environments: envs,
	}}
}

func verb(fields []string) Kind {
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

func parseArgs(args []string) (stacks, envs, rest []string, ok bool) {
	for _, a := range args {
		if a == "--" {
			break
		}
		// pflag treats -stack/-env as clustered shorts (-s tack, -e nv).
		if a == "-stack" || a == "-env" || strings.HasPrefix(a, "-stack=") || strings.HasPrefix(a, "-env=") {
			return nil, nil, nil, false
		}
	}
	fs := pflag.NewFlagSet("terraplane", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringArrayVarP(&stacks, "stack", "s", nil, "")
	fs.StringArrayVarP(&envs, "env", "e", nil, "")
	if err := fs.Parse(args); err != nil {
		return nil, nil, nil, false
	}
	if !validSelectors(stacks) || !validSelectors(envs) {
		return nil, nil, nil, false
	}
	return stacks, envs, fs.Args(), true
}

func validSelectors(values []string) bool {
	for _, v := range values {
		if v == "" || v == "=" || strings.HasPrefix(v, "-") {
			return false
		}
	}
	return true
}

func hasDoubleDash(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return true
		}
	}
	return false
}

func firstLine(body string) string {
	line := strings.TrimSpace(body)
	if i := strings.Index(line, "\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}
