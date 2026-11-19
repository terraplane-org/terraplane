package feedback_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/feedback"
	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func firstComment(job *models.Job, status models.JobStatus, output, errMsg string) string {
	return feedback.JobResultComments(job, status, output, errMsg)[0]
}

func TestPlanResultComments(t *testing.T) {
	job := &models.Job{
		StackName: "stg-foundation",
		Dir:       "terraform/environments/staging/foundation",
		CommitSHA: "abcdef0123456789",
		Action:    models.JobActionPlan,
	}

	t.Run("success with plan summary and output", func(t *testing.T) {
		output := "Terraform will perform the following actions:\n\nPlan: 1 to add, 2 to change, 3 to destroy.\n"
		got := firstComment(job, models.JobStatusSucceeded, output, "")

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
		got := firstComment(job, models.JobStatusSucceeded, output, "")
		require.Contains(t, got, "> [!NOTE]")
		require.Contains(t, got, "No infrastructure changes.")
		require.Contains(t, got, "terraplane apply -s stg-foundation")
		require.NotContains(t, got, "```diff")
	})

	t.Run("failure with error and no apply hint", func(t *testing.T) {
		got := firstComment(job, models.JobStatusFailed, "no plan line", "boom")
		require.Contains(t, got, "### `stg-foundation` · plan · ❌ failed")
		require.Contains(t, got, "> [!CAUTION]")
		require.Contains(t, got, "boom")
		require.Contains(t, got, "<summary>Output</summary>")
		require.NotContains(t, got, "Apply when ready")
		require.NotContains(t, got, "```diff")
	})

	t.Run("empty output and error", func(t *testing.T) {
		got := firstComment(&models.Job{StackName: "stg-foundation", Action: models.JobActionPlan}, models.JobStatusSucceeded, "   ", "  ")
		require.Equal(t, "### `stg-foundation` · plan · ✅ passed\n\nApply when ready:\n\n```\nterraplane apply -s stg-foundation\n```\n", got)
	})

	t.Run("extends fence for backticks in body", func(t *testing.T) {
		got := firstComment(job, models.JobStatusFailed, "", "error with ```` inside")
		require.Contains(t, got, "`````\nerror with ```` inside\n`````\n")
	})

	t.Run("zero destroys skip caution", func(t *testing.T) {
		output := "Plan: 0 to add, 1 to change, 0 to destroy.\n"
		got := firstComment(job, models.JobStatusSucceeded, output, "")
		require.Contains(t, got, "```diff\n+ 0 add\n  1 change\n- 0 destroy\n```")
		require.NotContains(t, got, "destroys")
	})

	t.Run("singular destroy wording", func(t *testing.T) {
		output := "Plan: 0 to add, 0 to change, 1 to destroy.\n"
		got := firstComment(job, models.JobStatusSucceeded, output, "")
		require.Contains(t, got, "destroys **1** resource.")
		require.NotContains(t, got, "resources.")
	})

	t.Run("short commit sha passthrough", func(t *testing.T) {
		short := &models.Job{StackName: "s", CommitSHA: "abc", Action: models.JobActionPlan}
		got := firstComment(short, models.JobStatusSucceeded, "", "")
		require.Contains(t, got, "`abc`")
	})

	t.Run("dir only meta", func(t *testing.T) {
		got := firstComment(&models.Job{StackName: "s", Dir: "terraform/x", Action: models.JobActionPlan}, models.JobStatusSucceeded, "", "")
		require.Contains(t, got, "`terraform/x`")
		require.NotContains(t, got, "@")
	})
}

func TestJobResultCommentsDispatchesOnAction(t *testing.T) {
	t.Run("apply action", func(t *testing.T) {
		job := &models.Job{
			StackName: "stg-foundation",
			Action:    models.JobActionApply,
			Dir:       "terraform/stg",
			CommitSHA: "deadbeef",
		}
		output := "Apply complete! Resources: 1 added, 0 changed, 0 destroyed.\n"
		got := firstComment(job, models.JobStatusSucceeded, output, "")
		require.Contains(t, got, "### `stg-foundation` · apply · ✅ passed")
	})

	t.Run("plan action by default", func(t *testing.T) {
		job := &models.Job{StackName: "stg-foundation"}
		got := firstComment(job, models.JobStatusSucceeded, "", "")
		require.Contains(t, got, "### `stg-foundation` · plan · ✅ passed")
	})
}

func TestApplyResultComments(t *testing.T) {
	job := &models.Job{
		StackName: "stg-foundation",
		Dir:       "terraform/stg",
		CommitSHA: "deadbeef",
		Action:    models.JobActionApply,
	}

	t.Run("success with apply summary", func(t *testing.T) {
		output := "Apply complete! Resources: 1 added, 2 changed, 0 destroyed.\n"
		got := firstComment(job, models.JobStatusSucceeded, output, "")
		require.Contains(t, got, "### `stg-foundation` · apply · ✅ passed")
		require.Contains(t, got, "`terraform/stg@deadbee`")
		require.Contains(t, got, "```diff\n+ 1 add\n  2 change\n- 0 destroy\n```")
		require.Contains(t, got, "<summary>Output</summary>")
		require.NotContains(t, got, "destroys")
		require.NotContains(t, got, "**terraplane**")
	})

	t.Run("failure with error only", func(t *testing.T) {
		got := firstComment(job, models.JobStatusFailed, "", "apply failed")
		require.Contains(t, got, "### `stg-foundation` · apply · ❌ failed")
		require.Contains(t, got, "apply failed")
		require.NotContains(t, got, "<details>")
	})
}

func TestUnlockResultComment(t *testing.T) {
	t.Run("with stack", func(t *testing.T) {
		got := feedback.UnlockResultComment("stg-foundation", models.JobStatusSucceeded, "")
		require.Equal(t, "### `stg-foundation` · unlock · ✅ passed\n", got)
	})

	t.Run("without stack", func(t *testing.T) {
		got := feedback.UnlockResultComment("", models.JobStatusFailed, "unlock failed")
		require.True(t, strings.HasPrefix(got, "### unlock · ❌ failed\n"))
		require.Contains(t, got, "unlock failed")
	})
}

func TestRunningStatusLabel(t *testing.T) {
	job := &models.Job{StackName: "stg-foundation", Action: models.JobActionPlan}
	got := firstComment(job, models.JobStatusRunning, "still going", "")
	require.Contains(t, got, "### `stg-foundation` · plan · ⌛ running")
	require.Contains(t, got, "<details open>")
	require.Contains(t, got, "<summary>Output (live)</summary>")
	require.Contains(t, got, "still going")
	require.NotContains(t, got, "Apply when ready")
}

func TestJobResultCommentsSplitsRunningOutput(t *testing.T) {
	job := &models.Job{StackName: "stg-foundation", Action: models.JobActionPlan}
	var b strings.Builder
	for b.Len() < 70000 {
		b.WriteString("live terraform output line for padding purposes only\n")
	}
	parts := feedback.JobResultComments(job, models.JobStatusRunning, b.String(), "")
	require.Greater(t, len(parts), 1)
	require.Contains(t, parts[0], "Output (live)")
	require.Contains(t, parts[1], "Output (live, continued)")
	require.Contains(t, parts[1], "output continued")
}

func TestJobResultCommentsContinuationWithoutStackName(t *testing.T) {
	job := &models.Job{Action: models.JobActionApply}
	var b strings.Builder
	for b.Len() < 70000 {
		b.WriteString("apply output line for padding purposes only\n")
	}
	parts := feedback.JobResultComments(job, models.JobStatusSucceeded, b.String(), "")
	require.Greater(t, len(parts), 1)
	require.Contains(t, parts[1], "### apply · output continued (2)")
}

func TestJobResultCommentsSplitsOversizedOutput(t *testing.T) {
	job := &models.Job{
		StackName: "stg-foundation",
		Dir:       "terraform/stg",
		CommitSHA: "abcdef0123456789",
		Action:    models.JobActionPlan,
	}
	// Build an output well over GitHub's 65536-character comment limit.
	var b strings.Builder
	b.WriteString("BEGIN\n")
	for b.Len() < 70000 {
		b.WriteString("line of terraform plan output that is fairly long for padding\n")
	}
	b.WriteString("Plan: 1 to add, 2 to change, 3 to destroy.\n")
	b.WriteString("END_MARKER\n")
	output := b.String()

	parts := feedback.JobResultComments(job, models.JobStatusSucceeded, output, "")
	require.Greater(t, len(parts), 1)

	var reconstructed strings.Builder
	for i, part := range parts {
		require.LessOrEqual(t, len([]rune(part)), 65000, "part %d exceeds GitHub limit", i+1)
		reconstructed.WriteString(extractFencedOutput(part))
	}
	require.Contains(t, parts[0], "```diff\n+ 1 add\n  2 change\n- 3 destroy\n```")
	require.Contains(t, parts[0], "BEGIN")
	require.Contains(t, parts[len(parts)-1], "terraplane apply -s stg-foundation")
	require.Contains(t, parts[1], "output continued")
	// Full raw output must be preserved across fenced sections — never truncated.
	// Leading/trailing whitespace is normalized before fencing.
	require.Equal(t, strings.TrimSpace(output), reconstructed.String())
}

func extractFencedOutput(comment string) string {
	var out strings.Builder
	for {
		start := strings.Index(comment, "<summary>")
		if start < 0 {
			break
		}
		comment = comment[start:]
		fenceStart := strings.Index(comment, "\n```")
		if fenceStart < 0 {
			break
		}
		comment = comment[fenceStart+1:] // at ```
		nl := strings.IndexByte(comment, '\n')
		if nl < 0 {
			break
		}
		fence := comment[:nl] // e.g. ```
		comment = comment[nl+1:]
		end := strings.Index(comment, "\n"+fence+"\n")
		if end < 0 {
			break
		}
		out.WriteString(comment[:end])
		comment = comment[end+len(fence)+2:]
	}
	return out.String()
}
