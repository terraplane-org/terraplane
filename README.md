

# Terraplane

PR-driven Terraform automation with remote agents.

[Website](https://terraplane.io) · [Docs](https://terraplane.io/docs) · [Quickstart](https://terraplane.io/docs/quickstart) · [Releases](https://github.com/terraplane-org/terraplane/releases)

---

# Still baking

This repository is so WIP it hurts. Nothing in here is designed for public consumption, nor should it be relied on for anything other than interest. It's a project I've wanted to make for a long time, but I've yet to determine if I have the capacity to see it through.

---



## What it is

Terraform belongs in the pull request, and the runner belongs inside your network. Terraplane keeps an Atlantis-style comment workflow (`plan` / `apply` / `unlock`) but splits into:

- an **orchestrator** — webhooks, jobs, locks, PR feedback
- **agents** — poll the orchestrator over HTTP and run Terraform where credentials and network access already live

Stacks live under environments in `terraplane.yaml` and route work to the right agent (`agent` on the environment, optional per-stack override). Details: [Why Terraplane](https://terraplane.io/docs/why), [How it works](https://terraplane.io/docs/how-it-works).

## Get started

**[Quickstart](https://terraplane.io/docs/quickstart)** is the path from zero to a PR plan. Everything else lives in the [docs](https://terraplane.io/docs) — configuration, deployment, local Make targets.

Helm:

```bash
helm install terraplane oci://ghcr.io/terraplane-org/charts/terraplane \
  --version 0.3.0 \
  -n terraplane --create-namespace \
  -f my-values.yaml
```

Chart values and split-install notes: `[charts/terraplane](charts/terraplane)`.

## Acknowledgements

Terraplane would not exist without [Atlantis](https://www.runatlantis.io/). The PR-comment UX is intentional; the remote-agent split is where Terraplane diverges.

## License

[MIT](LICENSE). Keep the copyright and permission notice; otherwise use it however you want.