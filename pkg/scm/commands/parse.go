package commands

import (
	"strings"

	"github.com/xyzjace/terraplane/pkg/scm/events"
)

func ParseComment(body string) (events.Kind, bool) {
	line := firstLine(body)
	if line == "" {
		return "", false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "terraplane") {
		return "", false
	}

	switch strings.ToLower(fields[1]) {
	case "plan":
		return events.KindPlan, true
	case "apply":
		return events.KindApply, true
	case "unlock":
		return events.KindUnlock, true
	default:
		return "", false
	}
}

// ParsePlanStacks extracts stack names from a plan comment such as
// "terraplane plan -s stg-apse2-foundation -stack stg-apse2-platform".
// Returns nil when no stack flags are present.
func ParsePlanStacks(comment string) []string {
	line := firstLine(comment)
	fields := strings.Fields(line)

	start := 0
	if len(fields) >= 2 && strings.EqualFold(fields[0], "terraplane") && strings.EqualFold(fields[1], "plan") {
		start = 2
	}

	var stacks []string
	for i := start; i < len(fields); i++ {
		switch fields[i] {
		case "-s", "-stack":
			i++
			if i < len(fields) {
				stacks = append(stacks, fields[i])
			}
		default:
			if name, ok := strings.CutPrefix(fields[i], "-s="); ok {
				stacks = append(stacks, name)
			} else if name, ok := strings.CutPrefix(fields[i], "-stack="); ok {
				stacks = append(stacks, name)
			}
		}
	}
	return stacks
}

func firstLine(body string) string {
	line := strings.TrimSpace(body)
	if idx := strings.Index(line, "\n"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	return line
}
