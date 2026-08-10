package command

import "testing"

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

func TestFlagValuesIgnoresNonVerb(t *testing.T) {
	// Exercises isVerb's false branch when the second token is not plan/apply/unlock.
	if got := stacks("terraplane nope -s stg-a"); len(got) != 1 || got[0] != "stg-a" {
		t.Fatalf("stacks(%q) = %v, want [stg-a]", "terraplane nope -s stg-a", got)
	}
}
