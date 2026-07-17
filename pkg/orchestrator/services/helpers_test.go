package services_test

import (
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/scm"
)

const twoStackYAML = `
stacks:
  - name: a
    agent: agent-a
    dir: stacks/a
  - name: b
    agent: agent-b
    dir: stacks/b
`

const sameAgentYAML = `
stacks:
  - name: a
    agent: shared
    dir: stacks/a
  - name: b
    agent: shared
    dir: stacks/b
`

func planCmd(comment string) command.PlanCommand {
	return command.ParseWebhook(&scm.Webhook{
		RepositorySlug: "acme/infra",
		PRNumber:       42,
		FullCommand:    comment,
		TriggeringUser: "jace",
		CommitSHA:      "abc123",
	}).Plan
}

func applyCmd(comment string) command.ApplyCommand {
	return command.ParseWebhook(&scm.Webhook{
		RepositorySlug: "acme/infra",
		PRNumber:       42,
		FullCommand:    comment,
		TriggeringUser: "jace",
		CommitSHA:      "abc123",
	}).Apply
}

func unlockCmd(comment string) command.UnlockCommand {
	return command.ParseWebhook(&scm.Webhook{
		RepositorySlug: "acme/infra",
		PRNumber:       42,
		FullCommand:    comment,
		TriggeringUser: "jace",
		CommitSHA:      "abc123",
	}).Unlock
}
