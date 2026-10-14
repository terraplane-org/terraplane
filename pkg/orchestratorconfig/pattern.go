package orchestratorconfig

import (
	"fmt"
	"path"
	"strings"
)

// validatePattern accepts repo patterns that use * and ? as single-segment
// wildcards. Character classes and escapes are rejected so overlap checks
// stay aligned with path.Match.
func validatePattern(pattern string) error {
	if pattern == "" || strings.TrimSpace(pattern) != pattern {
		return fmt.Errorf("repo pattern %q is invalid", pattern)
	}
	if strings.ContainsAny(pattern, `[\\`) {
		return fmt.Errorf("repo pattern %q: only * and ? wildcards are supported", pattern)
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return fmt.Errorf("repo pattern %q: %w", pattern, err)
	}
	return nil
}

// patternRank scores a pattern for most-specific-match.
// Exact patterns (no wildcards) outrank globs. Otherwise a higher literal
// count is more specific.
func patternRank(pattern string) (exact bool, literals int) {
	exact = true
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*', '?':
			exact = false
		default:
			literals++
		}
	}
	return exact, literals
}

func outranks(exact bool, literals int, otherExact bool, otherLiterals int) bool {
	if exact != otherExact {
		return exact
	}
	return literals > otherLiterals
}

func moreSpecific(a, b string) bool {
	aExact, aLit := patternRank(a)
	bExact, bLit := patternRank(b)
	return outranks(aExact, aLit, bExact, bLit)
}

// patternsOverlap reports whether any repo slug can match both patterns.
// * and ? do not cross /.
func patternsOverlap(a, b string) bool {
	as := strings.Split(a, "/")
	bs := strings.Split(b, "/")
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if !segmentIntersects(as[i], bs[i]) {
			return false
		}
	}
	return true
}

func segmentIntersects(a, b string) bool {
	if a == "*" || b == "*" {
		return true
	}
	if !strings.ContainsAny(a, "*?") && !strings.ContainsAny(b, "*?") {
		return a == b
	}

	symbols := []int{-1} // -1 is a character that is not a literal in either segment
	seen := map[byte]bool{}
	for _, segment := range []string{a, b} {
		for i := 0; i < len(segment); i++ {
			c := segment[i]
			if c == '*' || c == '?' || seen[c] {
				continue
			}
			seen[c] = true
			symbols = append(symbols, int(c))
		}
	}

	seenState := map[pairPos]bool{}
	var queue []pairPos
	enqueue := func(p pairPos) {
		if seenState[p] {
			return
		}
		seenState[p] = true
		queue = append(queue, p)
	}
	for _, p := range epsilonPairs(a, b, 0, 0) {
		enqueue(p)
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.i == len(a) && cur.j == len(b) {
			return true
		}
		for _, symbol := range symbols {
			ni, okA := consumeSegment(a, cur.i, symbol)
			nj, okB := consumeSegment(b, cur.j, symbol)
			if !okA || !okB {
				continue
			}
			for _, next := range epsilonPairs(a, b, ni, nj) {
				enqueue(next)
			}
		}
	}
	return false
}

func epsilonPairs(a, b string, i, j int) []pairPos {
	is := epsilon(a, i)
	js := epsilon(b, j)
	out := make([]pairPos, 0, len(is)*len(js))
	for _, ii := range is {
		for _, jj := range js {
			out = append(out, pairPos{ii, jj})
		}
	}
	return out
}

type pairPos struct{ i, j int }

// epsilon walks through '*' without consuming a character.
func epsilon(pattern string, start int) []int {
	seen := map[int]bool{}
	var pos []int
	var walk func(int)
	walk = func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		pos = append(pos, i)
		if i < len(pattern) && pattern[i] == '*' {
			walk(i + 1)
		}
	}
	walk(start)
	return pos
}

func consumeSegment(pattern string, i int, symbol int) (int, bool) {
	if i >= len(pattern) {
		return 0, false
	}
	switch pattern[i] {
	case '*':
		return i, true
	case '?':
		return i + 1, true
	default:
		if symbol >= 0 && byte(symbol) == pattern[i] {
			return i + 1, true
		}
		return 0, false
	}
}
