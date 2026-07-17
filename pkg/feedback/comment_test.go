package feedback_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func TestPlanResultComment(t *testing.T) {
	job := &models.Job{StackName: "stg-foundation"}

	t.Run("success with plan summary and output", func(t *testing.T) {
		output := "Terraform will perform the following actions:\n\nPlan: 1 to add, 2 to change, 3 to destroy.\n"
		got := feedback.PlanResultComment(job, true, output, "")
		require.Contains(t, got, "`stg-foundation` plan · ✅")
		require.Contains(t, got, "1 add · 2 change · 3 destroy")
		require.Contains(t, got, "<summary>plan</summary>")
		require.Contains(t, got, "apply with `terraplane apply -s stg-foundation`")
		require.NotContains(t, got, "❌")
	})

	t.Run("failure with error and no apply hint", func(t *testing.T) {
		got := feedback.PlanResultComment(job, false, "no plan line", "boom")
		require.Contains(t, got, "`stg-foundation` plan · ❌")
		require.Contains(t, got, "boom")
		require.Contains(t, got, "<summary>plan</summary>")
		require.NotContains(t, got, "apply with")
		require.NotContains(t, got, "add ·")
	})

	t.Run("empty output and error", func(t *testing.T) {
		got := feedback.PlanResultComment(job, true, "   ", "  ")
		require.Equal(t, "`stg-foundation` plan · ✅\n\napply with `terraplane apply -s stg-foundation`\n", got)
	})

	t.Run("extends fence for backticks in body", func(t *testing.T) {
		got := feedback.PlanResultComment(job, false, "", "error with ```` inside")
		require.Contains(t, got, "`````\nerror with ```` inside\n`````\n")
	})
}

func TestApplyResultComment(t *testing.T) {
	job := &models.Job{StackName: "stg-foundation"}

	t.Run("success with output", func(t *testing.T) {
		got := feedback.ApplyResultComment(job, true, "Apply complete!", "")
		require.Contains(t, got, "`stg-foundation` apply · ✅")
		require.Contains(t, got, "<summary>apply</summary>")
		require.Contains(t, got, "Apply complete!")
	})

	t.Run("failure with error only", func(t *testing.T) {
		got := feedback.ApplyResultComment(job, false, "", "apply failed")
		require.Contains(t, got, "`stg-foundation` apply · ❌")
		require.Contains(t, got, "apply failed")
		require.NotContains(t, got, "<details>")
	})
}

func TestUnlockResultComment(t *testing.T) {
	t.Run("with stack", func(t *testing.T) {
		got := feedback.UnlockResultComment("stg-foundation", true, "")
		require.Equal(t, "`stg-foundation` unlock · ✅\n", got)
	})

	t.Run("without stack", func(t *testing.T) {
		got := feedback.UnlockResultComment("", false, "unlock failed")
		require.True(t, strings.HasPrefix(got, "unlock · ❌\n"))
		require.Contains(t, got, "unlock failed")
	})
}
