package command

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanFlags(t *testing.T) {
	tests := []struct {
		comment string
		want    string
	}{
		{comment: "terraplane plan", want: ""},
		{comment: "terraplane plan -s stg-foundation", want: ""},
		{comment: "terraplane plan -e staging", want: ""},
		{comment: "terraplane plan -e staging -s stg-foundation -target=module.vpc", want: "-target=module.vpc"},
		{comment: "terraplane plan -s stg-foundation -target=module.vpc", want: "-target=module.vpc"},
		{comment: "terraplane plan -s stg-foundation -- -target=module.vpc", want: "-target=module.vpc"},
		{comment: "terraplane plan -s stg-foundation -- -target=module.vpc -var-file=terraform.tfvars", want: "-target=module.vpc -var-file=terraform.tfvars"},
	}

	for _, tt := range tests {
		if got := planFlags(tt.comment); got != tt.want {
			t.Fatalf("planFlags(%q) = %q, want %q", tt.comment, got, tt.want)
		}
	}
}

func TestJobID(t *testing.T) {
	plan := &Command{Kind: KindPlan}
	plan.Plan.JobID = "plan-job"
	require.Equal(t, "plan-job", plan.JobID())

	apply := &Command{Kind: KindApply}
	apply.Apply.JobID = "apply-job"
	require.Equal(t, "apply-job", apply.JobID())

	unlock := &Command{Kind: KindUnlock}
	unlock.Unlock.JobID = "unlock-job"
	require.Equal(t, "unlock-job", unlock.JobID())

	unknown := &Command{Kind: KindUnknown}
	require.Equal(t, "", unknown.JobID())
}

func TestFlagValuesIgnoresNonVerb(t *testing.T) {
	// Exercises isVerb's false branch when the second token is not plan/apply/unlock.
	if got := stacks("terraplane nope -s stg-a"); len(got) != 1 || got[0] != "stg-a" {
		t.Fatalf("stacks(%q) = %v, want [stg-a]", "terraplane nope -s stg-a", got)
	}
}
