package command_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/scm"
)

func TestParseWebhook(t *testing.T) {
	base := &scm.Webhook{
		RepositorySlug: "org/repo",
		PRNumber:       42,
		TriggeringUser: "jace",
		CommitSHA:      "abc123",
	}

	t.Run("plan", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane plan -s stg-a -stack stg-b -s=stg-c -stack=stg-d -target=module.x"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindPlan, cmd.Kind)
		require.Equal(t, "org/repo", cmd.Plan.Repo)
		require.Equal(t, 42, cmd.Plan.PRNumber)
		require.Equal(t, "jace", cmd.Plan.TriggerUser)
		require.Equal(t, "abc123", cmd.Plan.CommitSHA)
		require.Equal(t, w.FullCommand, cmd.Plan.RawComment)
		require.Equal(t, []string{"stg-a", "stg-b", "stg-c", "stg-d"}, cmd.Plan.Stacks)
		require.Equal(t, "-target=module.x", cmd.Plan.PlanFlags)
	})

	t.Run("plan with double dash flags", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane plan -s stg-a -- -target=module.vpc -var-file=x.tfvars"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindPlan, cmd.Kind)
		require.Equal(t, []string{"stg-a"}, cmd.Plan.Stacks)
		require.Equal(t, "-target=module.vpc -var-file=x.tfvars", cmd.Plan.PlanFlags)
	})

	t.Run("apply", func(t *testing.T) {
		w := *base
		w.FullCommand = "Terraplane APPLY -s stg-a"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindApply, cmd.Kind)
		require.Equal(t, []string{"stg-a"}, cmd.Apply.Stacks)
		require.Equal(t, "org/repo", cmd.Apply.Repo)
	})

	t.Run("unlock", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane unlock -stack stg-a"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindUnlock, cmd.Kind)
		require.Equal(t, []string{"stg-a"}, cmd.Unlock.Stacks)
	})

	t.Run("multiline uses first line", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane plan -s stg-a\nmore commentary"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindPlan, cmd.Kind)
		require.Equal(t, []string{"stg-a"}, cmd.Plan.Stacks)
	})

	t.Run("trailing stack flag without value", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane plan -s"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindPlan, cmd.Kind)
		require.Empty(t, cmd.Plan.Stacks)
	})

	t.Run("unknown", func(t *testing.T) {
		cases := []string{
			"",
			"hello",
			"terraplane",
			"terraplane something",
			"notterraplane plan",
		}
		for _, body := range cases {
			w := *base
			w.FullCommand = body
			cmd := command.ParseWebhook(&w)
			require.Equal(t, command.KindUnknown, cmd.Kind, "body=%q", body)
		}
	})
}
