package terraplaneconfig_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

func TestParseConfigFile(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cfg, err := terraplaneconfig.ParseConfigFile([]byte(`
stacks:
  - name: a
    agent: agent-dev
    dir: terraform/a
    terraform_version: "1.9.0"
  - name: b
    agent: agent-prod
    dir: terraform/b
`))
		require.NoError(t, err)
		require.Len(t, cfg.Stacks, 2)
		require.Equal(t, "a", cfg.Stacks[0].Name)
		require.Equal(t, "agent-dev", cfg.Stacks[0].Agent)
		require.Equal(t, "terraform/a", cfg.Stacks[0].Dir)
		require.Equal(t, "1.9.0", cfg.Stacks[0].TerraformVersion)
		require.Equal(t, "b", cfg.Stacks[1].Name)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		cfg, err := terraplaneconfig.ParseConfigFile([]byte("stacks: ["))
		require.Error(t, err)
		require.Nil(t, cfg)
		require.Contains(t, err.Error(), "parse terraplane config")
	})

	t.Run("empty document", func(t *testing.T) {
		cfg, err := terraplaneconfig.ParseConfigFile([]byte(""))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		require.Empty(t, cfg.Stacks)
	})
}

func TestResolveStacks(t *testing.T) {
	cfg := &terraplaneconfig.ConfigFile{
		Stacks: []terraplaneconfig.Stack{
			{Name: "a", Agent: "dev", Dir: "a"},
			{Name: "b", Agent: "prod", Dir: "b"},
		},
	}

	t.Run("no stacks configured", func(t *testing.T) {
		empty := &terraplaneconfig.ConfigFile{}
		got, err := empty.ResolveStacks(nil)
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), "no stacks configured")
	})

	t.Run("empty names returns all", func(t *testing.T) {
		got, err := cfg.ResolveStacks(nil)
		require.NoError(t, err)
		require.Equal(t, cfg.Stacks, got)

		got, err = cfg.ResolveStacks([]string{})
		require.NoError(t, err)
		require.Equal(t, cfg.Stacks, got)
	})

	t.Run("known names", func(t *testing.T) {
		got, err := cfg.ResolveStacks([]string{"b", "a"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, "b", got[0].Name)
		require.Equal(t, "a", got[1].Name)
	})

	t.Run("unknown name", func(t *testing.T) {
		got, err := cfg.ResolveStacks([]string{"a", "missing"})
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), `unknown stack "missing"`)
	})
}
