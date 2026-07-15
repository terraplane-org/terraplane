package feedback

import (
	"strings"
	"testing"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func TestPlanResultCommentSuccess(t *testing.T) {
	job := &models.Job{StackName: "stg-apse2-foundation"}
	output := "Refreshing state...\n\nPlan: 2 to add, 1 to change, 0 to destroy.\n\nChanges..."

	got := PlanResultComment(job, true, output, "")

	for _, want := range []string{
		"`stg-apse2-foundation` plan · ✅",
		"2 add · 1 change · 0 destroy",
		"<summary>plan</summary>",
		"apply with `terraplane apply -s stg-apse2-foundation`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("comment missing %q\n\n%s", want, got)
		}
	}
	for _, refuse := range []string{"succeeded", "### ", "To apply:", "Plan output", "**Plan:", "raw output"} {
		if strings.Contains(got, refuse) {
			t.Fatalf("comment still looks Atlantis-like, contains %q:\n%s", refuse, got)
		}
	}
}

func TestPlanResultCommentFailure(t *testing.T) {
	job := &models.Job{StackName: "stg-apse2-platform"}
	got := PlanResultComment(job, false, "Error: Unsupported argument", "terraform plan failed with exit code 1")

	for _, want := range []string{
		"`stg-apse2-platform` plan · ❌",
		"terraform plan failed with exit code 1",
		"<summary>plan</summary>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("comment missing %q\n\n%s", want, got)
		}
	}
	if strings.Contains(got, "apply with") {
		t.Fatalf("failure comment should not include apply hint:\n%s", got)
	}
}

func TestPlanResultCommentOmitsEmptySections(t *testing.T) {
	job := &models.Job{StackName: "empty-stack"}
	got := PlanResultComment(job, false, "", "boom")

	if strings.Contains(got, "<details>") {
		t.Fatalf("should omit empty plan output details:\n%s", got)
	}
	if !strings.Contains(got, "boom") {
		t.Fatalf("should include error:\n%s", got)
	}
}

func TestCodeFenceEscapesEmbeddedFences(t *testing.T) {
	body := "before\n```\ninside\n```\nafter"
	fence := codeFence(body)
	if fence != "````" {
		t.Fatalf("codeFence = %q, want ````", fence)
	}
}
