package auth

import "testing"

func TestBearerHeader(t *testing.T) {
	if got := BearerHeader("secret"); got != "Bearer secret" {
		t.Fatalf("BearerHeader = %q", got)
	}
}

func TestParseBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
		ok     bool
	}{
		{header: "Bearer secret", want: "secret", ok: true},
		{header: "  Bearer secret  ", want: "secret", ok: true},
		{header: "secret", ok: false},
		{header: "Bearer ", ok: false},
		{header: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := ParseBearerToken(tt.header)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("ParseBearerToken(%q) = (%q, %v), want (%q, %v)", tt.header, got, ok, tt.want, tt.ok)
		}
	}
}

func TestBearerTokenMatches(t *testing.T) {
	if !BearerTokenMatches("Bearer secret", "secret") {
		t.Fatal("expected match")
	}
	if BearerTokenMatches("Bearer wrong", "secret") {
		t.Fatal("expected mismatch")
	}
}
