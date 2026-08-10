package services_test

import (
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/scm"
)

const twoStackYAML = `
environments:
  - name: default
    stacks:
      - name: a
        agent: agent-a
        dir: stacks/a
      - name: b
        agent: agent-b
        dir: stacks/b
`

const sameAgentYAML = `
environments:
  - name: default
    agent: shared
    stacks:
      - name: a
        dir: stacks/a
      - name: b
        dir: stacks/b
`

const twoEnvYAML = `
environments:
  - name: staging
    agent: agent-a
    stacks:
      - name: a
        dir: stacks/a
      - name: b
        agent: agent-b
        dir: stacks/b
  - name: production
    agent: agent-prod
    stacks:
      - name: c
        dir: stacks/c
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
