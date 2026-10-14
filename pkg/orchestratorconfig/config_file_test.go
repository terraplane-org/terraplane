package orchestratorconfig_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/orchestratorconfig"
)

const validYAML = `
agents:
  agent-staging:
    token_env: ORCHESTRATOR_AGENT_STAGING_TOKEN
  agent-whatever:
    token_env: ORCHESTRATOR_AGENT_WHATEVER_TOKEN
  agent-generic:
    token_env: ORCHESTRATOR_AGENT_GENERIC_TOKEN
  agent-floop:
    token_env: ORCHESTRATOR_AGENT_FLOOP_TOKEN
  agent-something-different:
    token_env: ORCHESTRATOR_AGENT_SOMETHING_DIFFERENT_TOKEN
  agent-unused:
    token_env: ORCHESTRATOR_AGENT_UNUSED_TOKEN
repos:
  - repo: org/aws-infra
    environments:
      - name: staging
        agent: agent-staging
        stacks:
          - name: stg-apse2-foundation
            dir: terraform/environments/staging/ap-southeast-2/foundation
            tool_version: "1.9.0"
            tags:
              - foundation
              - something
          - name: stg-apse2-platform
            dir: terraform/environments/staging/ap-southeast-2/platform
          - name: stg-apse2-runtime
            dir: terraform/environments/staging/ap-southeast-2/runtime
          - name: stg-apse2-services
            dir: terraform/environments/staging/ap-southeast-2/services
          - name: stg-apse2-slow
            dir: terraform/environments/staging/ap-southeast-2/slow
      - name: floop
        agent: agent-floop
        stacks:
          - name: floop-apse2-foundation
            dir: terraform/environments/floop/ap-southeast-2/foundation
            tags:
              - foundation
  - repo: org/aws*
    environments:
      - name: whatever
        agent: agent-whatever
        stacks:
          - name: whatever-apse2-foundation
            dir: terraform/environments/whatever/ap-southeast-2/foundation
            agent: agent-something-different
          - name: whatever-apse2-slow
            dir: terraform/environments/whatever/ap-southeast-2/slow
  - repo: org/*
    environments:
      - name: generic
        agent: agent-generic
        stacks:
          - name: generic-apse2-foundation
            dir: terraform/environments/generic/ap-southeast-2/foundation
enabled: false
`

// singleRepo builds a minimal valid config with one agent and one repo entry.
func singleRepo(repo string) string {
	return `
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: ` + repo + `
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`
}

func TestParseConfigFile(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cfg, err := orchestratorconfig.ParseConfigFile([]byte(validYAML))
		require.NoError(t, err)
		require.False(t, cfg.Enabled)
		require.Equal(t, "ORCHESTRATOR_AGENT_STAGING_TOKEN", cfg.Agents["agent-staging"].TokenEnv)
		require.Len(t, cfg.Repos, 3)
		require.Equal(t, "org/aws-infra", cfg.Repos[0].Repo)
		require.Len(t, cfg.Repos[0].Environments, 2)
		require.Equal(t, "staging", cfg.Repos[0].Environments[0].Name)
		require.Equal(t, "agent-staging", cfg.Repos[0].Environments[0].Agent)
		stack := cfg.Repos[0].Environments[0].Stacks[0]
		require.Equal(t, "stg-apse2-foundation", stack.Name)
		require.Equal(t, "1.9.0", stack.ToolVersion)
		require.Equal(t, []string{"foundation", "something"}, stack.Tags)
		require.Equal(t, "org/aws*", cfg.Repos[1].Repo)
		require.Equal(t, "agent-something-different", cfg.Repos[1].Environments[0].Stacks[0].Agent)
	})

	t.Run("omitted enabled defaults to false", func(t *testing.T) {
		cfg, err := orchestratorconfig.ParseConfigFile([]byte(singleRepo("org/app")))
		require.NoError(t, err)
		require.False(t, cfg.Enabled)
	})

	t.Run("enabled true", func(t *testing.T) {
		cfg, err := orchestratorconfig.ParseConfigFile([]byte(singleRepo("org/app") + "enabled: true\n"))
		require.NoError(t, err)
		require.True(t, cfg.Enabled)
	})

	t.Run("unknown field is rejected", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
            tool_verison: "1.9.0"
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "tool_verison")
	})

	t.Run("invalid yaml", func(t *testing.T) {
		cfg, err := orchestratorconfig.ParseConfigFile([]byte("agents: ["))
		require.Error(t, err)
		require.Nil(t, cfg)
		require.Contains(t, err.Error(), "parse orchestrator config")
	})

	t.Run("empty document fails validation", func(t *testing.T) {
		cfg, err := orchestratorconfig.ParseConfigFile([]byte(""))
		require.Error(t, err)
		require.Nil(t, cfg)
		require.Contains(t, err.Error(), "no agents configured")
	})

	t.Run("no repos", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "no repos configured")
	})

	t.Run("missing token_env", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a: {}
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `agent "agent-a" is missing token_env`)
	})

	t.Run("duplicate token_env", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: SHARED
  agent-b:
    token_env: SHARED
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `duplicate token_env "SHARED" for agents "agent-a" and "agent-b"`)
	})

	t.Run("same stack name on different repos", func(t *testing.T) {
		cfg, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: alpha/one
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: foundation
            dir: stacks/one
  - repo: beta/two
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: foundation
            dir: stacks/two
`))
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})

	t.Run("duplicate stack names within a repo", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: shared
            dir: stacks/a
      - name: production
        agent: agent-a
        stacks:
          - name: shared
            dir: stacks/b
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `repo "org/app": stack name "shared" is not unique`)
	})

	t.Run("unknown agent", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-missing
        stacks:
          - name: app
            dir: stacks/app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `references unknown agent "agent-missing"`)
	})

	t.Run("unknown stack agent override", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
            agent: agent-missing
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `stack "app" in environment "staging" references unknown agent "agent-missing"`)
	})

	t.Run("missing agent", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        stacks:
          - name: app
            dir: stacks/app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "has no agent")
	})

	t.Run("missing dir", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing dir")
	})

	t.Run("duplicate environment names", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: a
            dir: stacks/a
      - name: staging
        agent: agent-a
        stacks:
          - name: b
            dir: stacks/b
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `duplicate environment name "staging"`)
	})

	t.Run("empty environment name", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: " "
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "environment name is required")
	})

	t.Run("environment with no stacks", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks: []
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `environment "staging" has no stacks`)
	})

	t.Run("empty stack name", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: ""
            dir: stacks/app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "stack name is required")
	})

	t.Run("empty tag", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
            tags:
              - " "
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "has an empty tag")
	})

	t.Run("duplicate tag", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
            tags:
              - foundation
              - foundation
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `has duplicate tag "foundation"`)
	})

	t.Run("ambiguous repo patterns", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  org/foo*:
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: a
            dir: stacks/a
  org/*foo:
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: b
            dir: stacks/b
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `repo patterns "org/*foo" and "org/foo*" are ambiguous`)
	})

	t.Run("unsupported wildcard", func(t *testing.T) {
		_, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  org/[abc]:
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: app
            dir: stacks/app
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "only * and ? wildcards are supported")
	})
}

func TestResolveStacks(t *testing.T) {
	cfg, err := orchestratorconfig.ParseConfigFile([]byte(validYAML))
	require.NoError(t, err)

	t.Run("invalid config", func(t *testing.T) {
		got, err := (&orchestratorconfig.ConfigFile{}).ResolveStacks("org/app", nil, nil, nil)
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), "no agents configured")
	})

	t.Run("repo is required", func(t *testing.T) {
		got, err := cfg.ResolveStacks("  ", nil, nil, nil)
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), "repo is required")
	})

	t.Run("exact pattern wins over globs", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 6)
		require.Equal(t, "stg-apse2-foundation", got[0].Name)
		require.Equal(t, "staging", got[0].Environment)
		require.Equal(t, "agent-staging", got[0].Agent)
		require.Equal(t, "ORCHESTRATOR_AGENT_STAGING_TOKEN", got[0].TokenEnv)
		require.Equal(t, "1.9.0", got[0].ToolVersion)
		require.Equal(t, []string{"foundation", "something"}, got[0].Tags)
		require.Equal(t, "floop-apse2-foundation", got[5].Name)
		require.Equal(t, "agent-floop", got[5].Agent)
		require.Equal(t, "ORCHESTRATOR_AGENT_FLOOP_TOKEN", got[5].TokenEnv)
		require.Equal(t, []string{"foundation"}, got[5].Tags)
		require.Empty(t, got[1].ToolVersion)
		require.Empty(t, got[1].Tags)
	})

	t.Run("longer glob wins over catch-all", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-other", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, "whatever-apse2-foundation", got[0].Name)
		require.Equal(t, "agent-something-different", got[0].Agent)
		require.Equal(t, "ORCHESTRATOR_AGENT_SOMETHING_DIFFERENT_TOKEN", got[0].TokenEnv)
		require.Equal(t, "agent-whatever", got[1].Agent)
		require.Equal(t, "ORCHESTRATOR_AGENT_WHATEVER_TOKEN", got[1].TokenEnv)
	})

	t.Run("catch-all matches remaining repos", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/other", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "generic-apse2-foundation", got[0].Name)
		require.Equal(t, "agent-generic", got[0].Agent)
		require.Equal(t, "ORCHESTRATOR_AGENT_GENERIC_TOKEN", got[0].TokenEnv)
	})

	t.Run("star matches empty suffix", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws", nil, nil, nil)
		require.NoError(t, err)
		require.Equal(t, "whatever-apse2-foundation", got[0].Name)
	})

	t.Run("no matching pattern", func(t *testing.T) {
		got, err := cfg.ResolveStacks("acme/infra", nil, nil, nil)
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), `no repo pattern matches "acme/infra"`)
	})

	t.Run("exact pattern beats equal-length glob", func(t *testing.T) {
		exact, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
  agent-b:
    token_env: TOKEN_B
repos:
  abc:
    environments:
      - name: exact
        agent: agent-a
        stacks:
          - name: from-exact
            dir: exact
  abc*:
    environments:
      - name: glob
        agent: agent-b
        stacks:
          - name: from-glob
            dir: glob
`))
		require.NoError(t, err)

		got, err := exact.ResolveStacks("abc", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "from-exact", got[0].Name)
		require.Equal(t, "TOKEN_A", got[0].TokenEnv)

		got, err = exact.ResolveStacks("abcd", nil, nil, nil)
		require.NoError(t, err)
		require.Equal(t, "from-glob", got[0].Name)
		require.Equal(t, "TOKEN_B", got[0].TokenEnv)
	})

	t.Run("by environment", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", nil, []string{"staging"}, nil)
		require.NoError(t, err)
		require.Len(t, got, 5)
		require.Equal(t, "stg-apse2-foundation", got[0].Name)
		require.Equal(t, "stg-apse2-slow", got[4].Name)
	})

	t.Run("by stack", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", []string{"stg-apse2-slow", "stg-apse2-foundation"}, nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, "stg-apse2-slow", got[0].Name)
		require.Equal(t, "stg-apse2-foundation", got[1].Name)
	})

	t.Run("union dedupes a stack already in the environment", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", []string{"floop-apse2-foundation"}, []string{"floop"}, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "floop-apse2-foundation", got[0].Name)
	})

	t.Run("union of stack and environment", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", []string{"floop-apse2-foundation"}, []string{"staging"}, nil)
		require.NoError(t, err)
		require.Len(t, got, 6)
		require.Equal(t, "floop-apse2-foundation", got[0].Name)
		require.Equal(t, "stg-apse2-foundation", got[1].Name)
		require.Equal(t, "stg-apse2-slow", got[5].Name)
	})

	t.Run("union of stack and several environments", func(t *testing.T) {
		multi, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: org/app
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: a
            dir: stacks/a
      - name: sandbox
        agent: agent-a
        stacks:
          - name: b
            dir: stacks/b
      - name: production
        agent: agent-a
        stacks:
          - name: c
            dir: stacks/c
`))
		require.NoError(t, err)
		got, err := multi.ResolveStacks("org/app", []string{"c"}, []string{"staging", "sandbox"}, nil)
		require.NoError(t, err)
		require.Equal(t, []string{"c", "a", "b"}, stackNames(got))
	})

	t.Run("by tag", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", nil, nil, []string{"foundation"})
		require.NoError(t, err)
		require.Equal(t, []string{"stg-apse2-foundation", "floop-apse2-foundation"}, stackNames(got))

		got, err = cfg.ResolveStacks("org/aws-infra", nil, nil, []string{"something"})
		require.NoError(t, err)
		require.Equal(t, []string{"stg-apse2-foundation"}, stackNames(got))
	})

	t.Run("tags combine with or", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", nil, nil, []string{"something", "foundation"})
		require.NoError(t, err)
		require.Equal(t, []string{"stg-apse2-foundation", "floop-apse2-foundation"}, stackNames(got))
	})

	t.Run("union of environment and tag", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", nil, []string{"floop"}, []string{"something"})
		require.NoError(t, err)
		require.Equal(t, []string{"floop-apse2-foundation", "stg-apse2-foundation"}, stackNames(got))
	})

	t.Run("union of stack environment and tag", func(t *testing.T) {
		got, err := cfg.ResolveStacks(
			"org/aws-infra",
			[]string{"stg-apse2-platform"},
			[]string{"floop"},
			[]string{"something"},
		)
		require.NoError(t, err)
		require.Equal(t, []string{"stg-apse2-platform", "floop-apse2-foundation", "stg-apse2-foundation"}, stackNames(got))
	})

	t.Run("unknown tag", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", []string{"stg-apse2-foundation"}, nil, []string{"nope"})
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), `unknown tag "nope"`)
	})

	t.Run("dedupes repeated tags", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", nil, nil, []string{"foundation", "foundation"})
		require.NoError(t, err)
		require.Equal(t, []string{"stg-apse2-foundation", "floop-apse2-foundation"}, stackNames(got))
	})

	t.Run("unknown environment", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", nil, []string{"nope"}, nil)
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), `unknown environment "nope"`)
	})

	t.Run("unknown stack", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", []string{"missing"}, nil, nil)
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), `unknown stack "missing"`)
	})

	t.Run("dedupes repeated selectors", func(t *testing.T) {
		got, err := cfg.ResolveStacks("org/aws-infra", []string{"stg-apse2-foundation", "stg-apse2-foundation"}, nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)

		got, err = cfg.ResolveStacks("org/aws-infra", nil, []string{"staging", "staging"}, nil)
		require.NoError(t, err)
		require.Len(t, got, 5)
	})

	t.Run("same stack name resolves per repo", func(t *testing.T) {
		shared, err := orchestratorconfig.ParseConfigFile([]byte(`
agents:
  agent-a:
    token_env: TOKEN_A
repos:
  - repo: alpha/one
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: foundation
            dir: stacks/one
  - repo: beta/two
    environments:
      - name: staging
        agent: agent-a
        stacks:
          - name: foundation
            dir: stacks/two
`))
		require.NoError(t, err)

		got, err := shared.ResolveStacks("alpha/one", nil, nil, nil)
		require.NoError(t, err)
		require.Equal(t, "stacks/one", got[0].Dir)

		got, err = shared.ResolveStacks("beta/two", nil, nil, nil)
		require.NoError(t, err)
		require.Equal(t, "stacks/two", got[0].Dir)
	})
}

func stackNames(stacks []orchestratorconfig.ResolvedStack) []string {
	names := make([]string, len(stacks))
	for i, stack := range stacks {
		names[i] = stack.Name
	}
	return names
}
