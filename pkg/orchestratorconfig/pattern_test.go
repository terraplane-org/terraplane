package orchestratorconfig

import "testing"

func TestPatternsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{a: "org/aws-infra", b: "org/aws*", want: true},
		{a: "org/aws*", b: "org/*", want: true},
		{a: "org/aws*", b: "com/*", want: false},
		{a: "org/foo*", b: "org/*foo", want: true},
		{a: "org/abc", b: "org/xyz", want: false},
		{a: "*", b: "foo", want: true},
		{a: "a*", b: "*b", want: true},
		{a: "a*", b: "b*", want: false},
		{a: "?", b: "a", want: true},
		{a: "?", b: "ab", want: false},
		{a: "*", b: "?", want: true},
		{a: "foo", b: "foo", want: true},
		{a: "foo", b: "bar", want: false},
		{a: "a/b", b: "a/*", want: true},
		{a: "a/b", b: "*", want: false},
		{a: "a*", b: "*a", want: true},
		{a: "pre*", b: "pref*", want: true},
		{a: "foo*", b: "*bar", want: true},
		{a: "abc", b: "abc*", want: true},
		{a: "team/svc*", b: "team/*svc", want: true},
		{a: "org/aws-????", b: "org/aws-*", want: true},
		{a: "alpha/*", b: "beta/*", want: false},
		{a: "", b: "*", want: true},
		{a: "**", b: "ab", want: true},
		{a: "a/?", b: "a/b", want: true},
		{a: "a/?", b: "a/bb", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.a+" vs "+tc.b, func(t *testing.T) {
			if got := patternsOverlap(tc.a, tc.b); got != tc.want {
				t.Fatalf("patternsOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if got := patternsOverlap(tc.b, tc.a); got != tc.want {
				t.Fatalf("patternsOverlap(%q, %q) = %v, want %v", tc.b, tc.a, got, tc.want)
			}
		})
	}
}
