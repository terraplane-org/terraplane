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
		require.Empty(t, cmd.Plan.Environments)
		require.Equal(t, "-target=module.x", cmd.Plan.PlanFlags)
	})

	t.Run("plan with environment", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane plan -e staging -env production -e=dev -env=qa -s stg-a"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindPlan, cmd.Kind)
		require.Equal(t, []string{"stg-a"}, cmd.Plan.Stacks)
		require.Equal(t, []string{"staging", "production", "dev", "qa"}, cmd.Plan.Environments)
		require.Empty(t, cmd.Plan.PlanFlags)
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

	t.Run("apply with environment", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane apply -e staging"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindApply, cmd.Kind)
		require.Empty(t, cmd.Apply.Stacks)
		require.Equal(t, []string{"staging"}, cmd.Apply.Environments)
	})

	t.Run("unlock", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane unlock -stack stg-a"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindUnlock, cmd.Kind)
		require.Equal(t, []string{"stg-a"}, cmd.Unlock.Stacks)
	})

	t.Run("unlock with environment", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane unlock -e staging"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindUnlock, cmd.Kind)
		require.Empty(t, cmd.Unlock.Stacks)
		require.Equal(t, []string{"staging"}, cmd.Unlock.Environments)
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
		require.Equal(t, command.KindUnknown, cmd.Kind)
	})

	t.Run("rejects positional stack name", func(t *testing.T) {
		for _, body := range []string{
			"terraplane plan stackname",
			"terraplane apply stackname",
			"terraplane unlock stackname",
			"terraplane plan -s stg-a stackname",
			"terraplane plan stackname -s stg-a",
		} {
			w := *base
			w.FullCommand = body
			cmd := command.ParseWebhook(&w)
			require.Equal(t, command.KindUnknown, cmd.Kind, "body=%q", body)
		}
	})

	t.Run("rejects apply and unlock terraform-style flags", func(t *testing.T) {
		for _, body := range []string{
			"terraplane apply -s stg-a -target=module.x",
			"terraplane unlock -s stg-a --force",
		} {
			w := *base
			w.FullCommand = body
			cmd := command.ParseWebhook(&w)
			require.Equal(t, command.KindUnknown, cmd.Kind, "body=%q", body)
		}
	})

	t.Run("rejects flag value that looks like a flag", func(t *testing.T) {
		w := *base
		w.FullCommand = "terraplane plan -s -e"
		cmd := command.ParseWebhook(&w)
		require.Equal(t, command.KindUnknown, cmd.Kind)
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
