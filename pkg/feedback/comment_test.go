package feedback_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func TestPlanResultComment(t *testing.T) {
	job := &models.Job{
		StackName: "stg-foundation",
		Dir:       "terraform/environments/staging/foundation",
		CommitSHA: "abcdef0123456789",
	}

	t.Run("success with plan summary and output", func(t *testing.T) {
		output := "Terraform will perform the following actions:\n\nPlan: 1 to add, 2 to change, 3 to destroy.\n"
		got := feedback.PlanResultComment(job, true, output, "")

		require.Contains(t, got, "### `stg-foundation` · plan · ✅ passed")
		require.Contains(t, got, "`terraform/environments/staging/foundation@abcdef0`")
		require.Contains(t, got, "```diff\n+ 1 add\n  2 change\n- 3 destroy\n```")
		require.Contains(t, got, "> [!CAUTION]")
		require.Contains(t, got, "destroys **3**")
		require.Contains(t, got, "<summary>Output</summary>")
		require.Contains(t, got, "terraplane apply -s stg-foundation")
		require.NotContains(t, got, "**terraplane**")
		require.NotContains(t, got, "❌")
	})

	t.Run("no changes", func(t *testing.T) {
		output := "No changes. Your infrastructure matches the configuration.\n"
		got := feedback.PlanResultComment(job, true, output, "")
		require.Contains(t, got, "> [!NOTE]")
		require.Contains(t, got, "No infrastructure changes.")
		require.Contains(t, got, "terraplane apply -s stg-foundation")
		require.NotContains(t, got, "```diff")
	})

	t.Run("failure with error and no apply hint", func(t *testing.T) {
		got := feedback.PlanResultComment(job, false, "no plan line", "boom")
		require.Contains(t, got, "### `stg-foundation` · plan · ❌ failed")
		require.Contains(t, got, "> [!CAUTION]")
		require.Contains(t, got, "boom")
		require.Contains(t, got, "<summary>Output</summary>")
		require.NotContains(t, got, "Apply when ready")
		require.NotContains(t, got, "```diff")
	})

	t.Run("empty output and error", func(t *testing.T) {
		got := feedback.PlanResultComment(&models.Job{StackName: "stg-foundation"}, true, "   ", "  ")
		require.Equal(t, "### `stg-foundation` · plan · ✅ passed\n\nApply when ready:\n\n```\nterraplane apply -s stg-foundation\n```\n", got)
	})

	t.Run("extends fence for backticks in body", func(t *testing.T) {
		got := feedback.PlanResultComment(job, false, "", "error with ```` inside")
		require.Contains(t, got, "`````\nerror with ```` inside\n`````\n")
	})

	t.Run("zero destroys skip caution", func(t *testing.T) {
		output := "Plan: 0 to add, 1 to change, 0 to destroy.\n"
		got := feedback.PlanResultComment(job, true, output, "")
		require.Contains(t, got, "```diff\n+ 0 add\n  1 change\n- 0 destroy\n```")
		require.NotContains(t, got, "destroys")
	})

	t.Run("singular destroy wording", func(t *testing.T) {
		output := "Plan: 0 to add, 0 to change, 1 to destroy.\n"
		got := feedback.PlanResultComment(job, true, output, "")
		require.Contains(t, got, "destroys **1** resource.")
		require.NotContains(t, got, "resources.")
	})

	t.Run("short commit sha passthrough", func(t *testing.T) {
		short := &models.Job{StackName: "s", CommitSHA: "abc"}
		got := feedback.PlanResultComment(short, true, "", "")
		require.Contains(t, got, "`abc`")
	})

	t.Run("dir only meta", func(t *testing.T) {
		got := feedback.PlanResultComment(&models.Job{StackName: "s", Dir: "terraform/x"}, true, "", "")
		require.Contains(t, got, "`terraform/x`")
		require.NotContains(t, got, "@")
	})
}

func TestApplyResultComment(t *testing.T) {
	job := &models.Job{
		StackName: "stg-foundation",
		Dir:       "terraform/stg",
		CommitSHA: "deadbeef",
	}

	t.Run("success with apply summary", func(t *testing.T) {
		output := "Apply complete! Resources: 1 added, 2 changed, 0 destroyed.\n"
		got := feedback.ApplyResultComment(job, true, output, "")
		require.Contains(t, got, "### `stg-foundation` · apply · ✅ passed")
		require.Contains(t, got, "`terraform/stg@deadbee`")
		require.Contains(t, got, "```diff\n+ 1 add\n  2 change\n- 0 destroy\n```")
		require.Contains(t, got, "<summary>Output</summary>")
		require.NotContains(t, got, "destroys")
		require.NotContains(t, got, "**terraplane**")
	})

	t.Run("failure with error only", func(t *testing.T) {
		got := feedback.ApplyResultComment(job, false, "", "apply failed")
		require.Contains(t, got, "### `stg-foundation` · apply · ❌ failed")
		require.Contains(t, got, "apply failed")
		require.NotContains(t, got, "<details>")
	})
}

func TestUnlockResultComment(t *testing.T) {
	t.Run("with stack", func(t *testing.T) {
		got := feedback.UnlockResultComment("stg-foundation", true, "")
		require.Equal(t, "### `stg-foundation` · unlock · ✅ passed\n", got)
	})

	t.Run("without stack", func(t *testing.T) {
		got := feedback.UnlockResultComment("", false, "unlock failed")
		require.True(t, strings.HasPrefix(got, "### unlock · ❌ failed\n"))
		require.Contains(t, got, "unlock failed")
	})
}
