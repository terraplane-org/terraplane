package services

import (
	"context"

	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

// anyConnectedAgent reports whether at least one agent named by the resolved
// stacks is currently registered. Unique agent IDs are checked once each.
func anyConnectedAgent(ctx context.Context, registry agentsession.Registry, stacks []terraplaneconfig.ResolvedStack) (bool, error) {
	seen := make(map[string]struct{}, len(stacks))
	for _, stack := range stacks {
		if _, ok := seen[stack.Agent]; ok {
			continue
		}
		seen[stack.Agent] = struct{}{}

		agent, err := registry.Get(ctx, stack.Agent)
		if err != nil {
			return false, err
		}
		if agent != nil {
			return true, nil
		}
	}
	return false, nil
}

func uniqueAgentNames(stacks []terraplaneconfig.ResolvedStack) []string {
	seen := make(map[string]struct{}, len(stacks))
	names := make([]string, 0, len(stacks))
	for _, stack := range stacks {
		if _, ok := seen[stack.Agent]; ok {
			continue
		}
		seen[stack.Agent] = struct{}{}
		names = append(names, stack.Agent)
	}
	return names
}
