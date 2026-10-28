package orchestratorconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/orchestratorconfig"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

func TestGatesAndApplyRequirements(t *testing.T) {
	const yaml = `
agents:
  agent-a:
    token_env: TOKEN_A
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
        environments: [staging]
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`

	cfg, err := orchestratorconfig.ParseConfigFile([]byte(yaml))
	require.NoError(t, err)
	require.True(t, cfg.PlanGateEnabled("Org/Aws-Infra"))
	require.False(t, cfg.PlanGateEnabled("org/other"))

	prod := terraplaneconfig.ResolvedStack{Environment: "production", Name: "prod-foundation"}
	slow := terraplaneconfig.ResolvedStack{Environment: "staging", Name: "stg-apse2-slow"}
	tagged := terraplaneconfig.ResolvedStack{Environment: "staging", Name: "stg-app", Tags: []string{"protected"}}
	staging := terraplaneconfig.ResolvedStack{Environment: "staging", Name: "stg-app", Tags: []string{"fast"}}

	require.True(t, cfg.ApplyRequiresApproval("org/aws-infra", prod))
	require.True(t, cfg.ApplyRequiresApproval("org/aws-infra", slow))
	require.True(t, cfg.ApplyRequiresApproval("org/aws-infra", tagged))
	require.False(t, cfg.ApplyRequiresApproval("org/aws-infra", staging))
	require.False(t, cfg.ApplyRequiresApproval("org/other", prod))
}

func TestGateValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unknown gate",
			yaml: gateYAML("type: apply"),
			want: "unknown gate type",
		},
		{
			name: "duplicate plan gate",
			yaml: gateYAML("type: plan\n      - type: plan"),
			want: "listed more than once",
		},
		{
			name: "missing requirement selector",
			yaml: requirementYAML("type: approved"),
			want: "must set environments, stacks, or tags",
		},
		{
			name: "unknown requirement",
			yaml: requirementYAML("type: mergeable\n        environments: [production]"),
			want: "unknown apply requirement type",
		},
		{
			name: "duplicate stack selector",
			yaml: requirementYAML("type: approved\n        stacks: [a, a]"),
			want: "duplicate stack",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := orchestratorconfig.ParseConfigFile([]byte(tc.yaml))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadFile(t *testing.T) {
	cfg, err := orchestratorconfig.LoadFile("  ")
	require.NoError(t, err)
	require.Nil(t, cfg)
	require.False(t, cfg.PlanGateEnabled("org/app"))

	missing, err := orchestratorconfig.LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	require.NoError(t, err)
	require.Nil(t, missing)

	path := filepath.Join(t.TempDir(), "orchestrator.yaml")
	require.NoError(t, os.WriteFile(path, []byte(gateYAML("type: plan")), 0o644))
	loaded, err := orchestratorconfig.LoadFile(path)
	require.NoError(t, err)
	require.True(t, loaded.PlanGateEnabled("org/app"))
}

func gateYAML(gates string) string {
	return `
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    gates:
      - ` + gates + `
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`
}

func requirementYAML(req string) string {
	return `
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    apply_requirements:
      - ` + req + `
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`
}
