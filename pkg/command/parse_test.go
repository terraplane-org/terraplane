package command

import "testing"

func TestPlanFlags(t *testing.T) {
	tests := []struct {
		comment string
		want    string
	}{
		{comment: "terraplane plan", want: ""},
		{comment: "terraplane plan -s stg-foundation", want: ""},
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
