package commands

import (
	"strings"

	"github.com/xyzjace/terraplane/pkg/scm/events"
)

func ParseComment(body string) (events.Kind, bool) {
	line := strings.TrimSpace(body)
	if line == "" {
		return "", false
	}
	if idx := strings.Index(line, "\n"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
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
