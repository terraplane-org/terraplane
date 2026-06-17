package commands

import "testing"

func TestParsePlanStacks(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    []string
	}{
		{
			name:    "no stack flags",
			comment: "terraplane plan",
			want:    nil,
		},
		{
			name:    "single short flag",
			comment: "terraplane plan -s stg-apse2-foundation",
			want:    []string{"stg-apse2-foundation"},
		},
		{
			name:    "multiple flags",
			comment: "terraplane plan -s stg-apse2-foundation -stack stg-apse2-platform",
			want:    []string{"stg-apse2-foundation", "stg-apse2-platform"},
		},
		{
			name:    "equals form",
			comment: "terraplane plan -s=stg-apse2-foundation -stack=stg-apse2-platform",
			want:    []string{"stg-apse2-foundation", "stg-apse2-platform"},
		},
		{
			name:    "first line only",
			comment: "terraplane plan -s stg-apse2-foundation\nextra text",
			want:    []string{"stg-apse2-foundation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePlanStacks(tt.comment)
			if len(got) != len(tt.want) {
				t.Fatalf("ParsePlanStacks() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ParsePlanStacks() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
