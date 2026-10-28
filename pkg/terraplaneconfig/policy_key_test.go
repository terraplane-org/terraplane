package terraplaneconfig

import "testing"

func TestFindPolicyKeyNil(t *testing.T) {
	if got := findPolicyKey(nil); got != "" {
		t.Fatalf("findPolicyKey(nil) = %q", got)
	}
}
