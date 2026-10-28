# Merge gates

Terraplane posts a status on the pull request commit. The git host can require that status before a merge. Terraplane also refuses `terraplane apply` for selected stacks until the pull request is approved.

These are two settings. A gate is the status. A requirement is the apply check. Both live on a repo entry in the orchestrator config. `terraplane.yaml` lists stacks. It does not set gates or apply requirements.

The orchestrator loads this file from `ORCHESTRATOR_CONFIG_PATH`. An empty path leaves both features off.

## Gates

`gates` is a list on the repo entry. The only type in this version is `plan`.

```yaml
repos:
  - repo: org/aws-infra
    gates:
      - type: plan
    environments:
      - name: staging
        agent: agent-staging
        stacks:
          - name: stg-apse2-foundation
            dir: terraform/environments/staging/ap-southeast-2/foundation
```

When someone runs `terraplane plan`, Terraplane posts:

- `terraplane/plan: <stack>` for each stack in that plan
- `terraplane/plan` for those stacks together

The summary status stays pending while any of those plans is still running. It fails if one of them fails. It passes when every plan for that commit has passed. A stack that nobody planned is left out. `terraplane plan -s one` on a five-stack repo covers that one stack.

The status name is fixed. Put `terraplane/plan` in the host's required-check list. The per-stack statuses show which stack is pending or failing.

Terraplane posts the status after a plan exists. If an admin marks `terraplane/plan` required, the host waits for it before a merge, including on a pull request where nobody has planned. Leave it off that list and the merge stays available. The status still shows on the commit.

A failed status update is logged. It does not fail the plan job.

## Apply requirements

`apply_requirements` is a list on the same repo entry. The only type in this version is `approved`. Each rule names environments, stacks, or tags. Names in one field match with OR. Fields on one rule match with AND. A stack matches if any rule matches it.

The names are the names in the pull request's `terraplane.yaml`. A new stack in `production` is included by an environment rule. The repo entry still has its own `environments` block, because that block is how the server file names agents.

```yaml
repos:
  - repo: org/aws-infra
    gates:
      - type: plan
    apply_requirements:
      - type: approved
        environments: [production]
      - type: approved
        stacks: [stg-apse2-slow]
      - type: approved
        tags: [protected]
```

`terraplane apply` on a matching stack reads the pull request reviews. The apply starts when someone other than the author has approved, and that reviewer's latest decisive review is still an approval. A later request for changes, or a dismissed review, drops that reviewer's approval. Another reviewer's approval still counts.

If the pull request is not approved, Terraplane skips that stack, writes a comment, and does not take the apply lock. Other stacks in the same command still run. If the review lookup fails, the command stops and no further apply jobs are created.

## Later

These stay out of the config until they exist:

- `type: apply` and `type: policy` gates
- a scope that includes every stack in the file
- auto-plan on pull request open and on push
- `allowed_overrides`, so a repo can replace `apply_requirements` for one environment or stack

An empty `apply_requirements` value in the repo file is not an override. The key is rejected. The server rule remains in force.
