# Terraplane Helm Chart

PR-driven Terraform automation with remote agents.

Agents usually run in a **different** network/cluster than the orchestrator (private infra vs public webhooks). Use two Helm releases of this chart, or disable the half you do not want.

## Install

### Orchestrator release

```bash
kubectl create namespace terraplane

kubectl -n terraplane create secret generic terraplane-orchestrator \
  --from-literal=DATABASE_URL='postgres://user:pass@host:5432/terraplane?sslmode=require' \
  --from-literal=DATABASE_DRIVER=postgres \
  --from-literal=SHARED_AUTH_TOKEN='change-me' \
  --from-literal=ORCHESTRATOR_GITHUB_ACCESS_TOKEN='ghp_...' \
  --from-literal=ORCHESTRATOR_GITHUB_WEBHOOK_SECRET='...'

helm install terraplane-orch oci://ghcr.io/terraplane-org/charts/terraplane \
  --version 0.3.0 \
  -n terraplane \
  -f orch-values.yaml
```

```yaml
# orch-values.yaml
# image.tag defaults to Chart.appVersion when omitted

orchestrator:
  enabled: true
  envFrom:
    - secretRef:
        name: terraplane-orchestrator
  ingress:
    enabled: true
    hosts:
      - host: terraplane.example.com
        paths:
          - path: /
            pathType: Prefix

agents: []
```

### Agents release (separate cluster/namespace)

```bash
# Create the namespace first so secrets can be placed into it
kubectl create namespace terraplane-agents

kubectl -n terraplane-agents create secret generic terraplane-agent \
  --from-literal=SHARED_AUTH_TOKEN='change-me'

kubectl -n terraplane-agents create secret generic agent-dev-ssh \
  --from-file=ssh-private-key=./id_ed25519_dev

kubectl -n terraplane-agents create secret generic agent-prod-ssh \
  --from-file=ssh-private-key=./id_ed25519_prod

helm install terraplane-agents oci://ghcr.io/terraplane-org/charts/terraplane \
  --version 0.3.0 \
  -n terraplane-agents \
  -f agents-values.yaml
```

```yaml
# agents-values.yaml
namespaceOverride: terraplane-agents

orchestrator:
  enabled: false

agentDefaults:
  orchestratorURL: https://terraplane.example.com
  envFrom:
    - secretRef:
        name: terraplane-agent

agents:
  - name: agent-dev
    sshKey:
      secretName: agent-dev-ssh
  - name: agent-prod
    sshKey:
      secretName: agent-prod-ssh
    persistence:
      size: 50Gi
```

Cloud secret managers (AWS Secrets Manager, GCP Secret Manager, Vault, etc.) are **out of band**: sync into Kubernetes Secrets with External Secrets Operator / CSI / your own tooling, then reference those Secret names from values.

To attach arbitrary volumes (for example a Secrets Store CSI mount that drives `secretObjects` sync) or a workload-identity ServiceAccount, use `serviceAccountName`, `extraVolumes`, and `extraVolumeMounts`. The chart does not create ServiceAccounts or cloud-specific secret resources.

```yaml
agentDefaults:
  orchestratorURL: https://orchestrator.example.com
  serviceAccountName: terraplane-agent
  envFrom:
    - secretRef:
        name: terraplane-agent
  sshKey:
    secretName: terraplane-agent-ssh
    secretKey: ssh-private-key
    mountPath: /etc/terraplane/git_ssh_key
  # Platform-provided volume so secret sync / sidecar CSI can run on this pod
  extraVolumes:
    - name: secrets-store
      csi:
        driver: secrets-store.csi.k8s.io
        readOnly: true
        volumeAttributes:
          secretProviderClass: terraplane-agent-secrets
  extraVolumeMounts:
    - name: secrets-store
      mountPath: /mnt/secrets-store
      readOnly: true
agents:
  - name: stg-apse2
```

Generic non-CSI example:

```yaml
agentDefaults:
  extraVolumes:
    - name: config
      configMap:
        name: agent-extra-config
  extraVolumeMounts:
    - name: config
      mountPath: /etc/agent-config
      readOnly: true
```

`agentDefaults.extraVolumes` / `extraVolumeMounts` are concatenated with the matching per-agent lists (same merge rule as `envFrom`). Per-agent `serviceAccountName` wins when non-empty.

Image tags follow release semver (`ghcr.io/terraplane-org/terraplane:<version>`). Chart `appVersion` matches the release; leave `image.tag` empty to use it. See [docs/release.md](../../docs/release.md).

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| namespaceOverride | string | `"terraplane"` | Override the release namespace. Set to `""` to use `helm -n` / Release.Namespace only. |
| image.repository | string | `"ghcr.io/terraplane-org/terraplane"` | Container image repository |
| image.tag | string | `""` | Image tag (defaults to Chart.AppVersion when empty) |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| imagePullSecrets | list | `[]` | Optional image pull secrets |
| commonLabels | object | `{}` | Extra labels applied to all resources |
| orchestrator.enabled | bool | `true` | Deploy the orchestrator. Set false for an agents-only release. |
| orchestrator.replicaCount | int | `1` | Number of orchestrator replicas. Each replica dispatches to agents connected to it. |
| orchestrator.migrate.enabled | bool | `true` | Run `terraplane db migrate` as an initContainer before the orchestrator starts |
| orchestrator.serviceAccountName | string | `""` | Existing ServiceAccount name (chart does not create it). Omit to use the namespace default. |
| orchestrator.env | object | `{}` | Extra environment variables for the orchestrator (non-secret config) |
| orchestrator.envFrom | list | `[]` | Load env from existing ConfigMaps/Secrets (chart does not create secrets) |
| orchestrator.extraVolumes | list | `[]` | Extra PodSpec volumes (pass-through; chart does not interpret volume types) |
| orchestrator.extraVolumeMounts | list | `[]` | Extra volumeMounts on the orchestrator container (pass-through) |
| orchestrator.resources | object | `{}` | Pod resources |
| orchestrator.service.type | string | `"ClusterIP"` | Kubernetes Service type |
| orchestrator.service.port | int | `8080` | Service / container port |
| orchestrator.ingress.enabled | bool | `false` | Enable Ingress for the orchestrator (webhook + health) |
| orchestrator.ingress.className | string | `""` | Ingress class name |
| orchestrator.ingress.annotations | object | `{}` | Ingress annotations |
| orchestrator.ingress.hosts | list | `[]` | Ingress hosts |
| orchestrator.ingress.tls | list | `[]` | Ingress TLS |
| agentDefaults.env | object | `{}` | Shared env for all agents (merged under per-agent env) |
| agentDefaults.envFrom | list | `[]` | Shared envFrom for all agents (concatenated with per-agent envFrom) |
| agentDefaults.orchestratorURL | string | `""` | HTTP(S) base URL agents use to reach the orchestrator (required when agents is non-empty). Example: `https://terraplane.example.com` |
| agentDefaults.serviceAccountName | string | `""` | Existing ServiceAccount for agent pods (chart does not create it). Per-agent non-empty override wins. |
| agentDefaults.extraVolumes | list | `[]` | Extra PodSpec volumes for all agents (concatenated with `agents[].extraVolumes`; pass-through) |
| agentDefaults.extraVolumeMounts | list | `[]` | Extra volumeMounts for all agents (concatenated with `agents[].extraVolumeMounts`; after work/ssh) |
| agentDefaults.workDir | string | `"/var/terraplane/work"` | Agent working directory (workspace + terraform bins) |
| agentDefaults.resources | object | `{}` | Default pod resources for agents |
| agentDefaults.sshKey.secretName | string | `""` | Existing Secret name containing the deploy key (required for private repos) |
| agentDefaults.sshKey.secretKey | string | `"ssh-private-key"` | Key within the Secret |
| agentDefaults.sshKey.mountPath | string | `"/etc/terraplane/git_ssh_key"` | Absolute path where the key is mounted in the container |
| agentDefaults.persistence.enabled | bool | `true` | Create a PVC per agent for plan/apply workspace reuse. false uses emptyDir |
| agentDefaults.persistence.size | string | `"20Gi"` | PVC size |
| agentDefaults.persistence.storageClassName | string | `""` | StorageClass name (empty = cluster default) |
| agentDefaults.persistence.accessMode | string | `"ReadWriteOnce"` | PVC access mode |
| agents | list | `[]` | Agent definitions. Each entry becomes a Deployment (replicas=1) with AGENT_ID = name. Optional per-agent: `serviceAccountName`, `extraVolumes`, `extraVolumeMounts`, `env`, `envFrom`, `sshKey`, `persistence`, `resources`, `workDir`. |
