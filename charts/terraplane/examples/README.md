# Helm examples

Copy-pasteable values for the [Terraplane chart](../). Secrets are created out of band; each `values.yaml` header lists the matching `kubectl create secret` commands.

| Example | Use when |
|---------|----------|
| [orchestrator](orchestrator/) | Public orchestrator only; agents run elsewhere |
| [agent](agent/) | Private agents only; orchestrator URL is remote |
| [combined](combined/) | Lab / single cluster: orchestrator + agent together |

## Orchestrator

```bash
kubectl create namespace terraplane
# create terraplane-orchestrator secret (see values.yaml header)

helm install terraplane-orch oci://ghcr.io/terraplane-org/charts/terraplane \
  --version 0.3.0 \
  -n terraplane \
  -f orchestrator/values.yaml
```

## Agents

```bash
kubectl create namespace terraplane-agents
# create terraplane-agent + SSH secrets (see values.yaml header)

helm install terraplane-agents oci://ghcr.io/terraplane-org/charts/terraplane \
  --version 0.3.0 \
  -n terraplane-agents \
  -f agent/values.yaml
```

## Combined (same release)

Use release name `terraplane` so `agentDefaults.orchestratorURL` matches the in-cluster Service (`terraplane-orchestrator`).

```bash
kubectl create namespace terraplane
# create orchestrator, agent, and SSH secrets (see values.yaml header)

helm install terraplane oci://ghcr.io/terraplane-org/charts/terraplane \
  --version 0.3.0 \
  -n terraplane \
  -f combined/values.yaml
```

From a local checkout you can also `-f charts/terraplane/examples/<name>/values.yaml` against a chart path or OCI package.
