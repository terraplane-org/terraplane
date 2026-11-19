package feedback

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

// GitHub rejects issue/PR comment bodies over 65536 characters (Unicode code
// points). Stay under that with headroom for wrapper markdown and continuation
// headers. Oversized terraform output is split across multiple comments — never
// truncated — so reviewers always see the full log.
const maxCommentRunes = 65000

var (
	planSummaryRE = regexp.MustCompile(
		`(?m)^Plan:\s+(\d+)\s+to\s+add,\s+(\d+)\s+to\s+change,\s+(\d+)\s+to\s+destroy\.?$`,
	)
	applySummaryRE = regexp.MustCompile(
		`(?m)^Apply complete! Resources:\s+(\d+)\s+added,\s+(\d+)\s+changed,\s+(\d+)\s+destroyed\.?$`,
	)
	noChangesRE = regexp.MustCompile(`(?i)No changes\.\s+Your infrastructure matches the configuration\.`)
)

type resourceDelta struct {
	add, change, destroy int
}

// JobResultComments formats a plan or apply job result as one or more GitHub PR
// comment bodies. Together they contain the full terraform output and error text
// with nothing discarded. Always returns at least one body.
func JobResultComments(job *models.Job, status models.JobStatus, output, errMsg string) []string {
	switch job.Action {
	case models.JobActionApply:
		return applyResultComments(job, status, output, errMsg)
	default:
		return planResultComments(job, status, output, errMsg)
	}
}

func planResultComments(job *models.Job, status models.JobStatus, output, errMsg string) []string {
	success := status == models.JobStatusSucceeded

	var prelude strings.Builder
	writeHeader(&prelude, "plan", job.StackName, status)
	writeMeta(&prelude, job)

	if delta, fromSummary := parsePlanDelta(output); fromSummary {
		writeDelta(&prelude, delta)
	} else if success && noChangesRE.MatchString(output) {
		prelude.WriteString("\n> [!NOTE]\n")
		prelude.WriteString("> No infrastructure changes.\n")
	}

	var footer string
	if success {
		footer = fmt.Sprintf("\nApply when ready:\n\n```\nterraplane apply -s %s\n```\n", job.StackName)
	}

	return splitResultComments(job.StackName, "plan", status, prelude.String(), errMsg, output, footer)
}

func applyResultComments(job *models.Job, status models.JobStatus, output, errMsg string) []string {
	var prelude strings.Builder
	writeHeader(&prelude, "apply", job.StackName, status)
	writeMeta(&prelude, job)

	if delta, ok := parseApplyDelta(output); ok {
		writeDelta(&prelude, delta)
	}

	return splitResultComments(job.StackName, "apply", status, prelude.String(), errMsg, output, "")
}

// UnlockResultComment formats an unlock result as a GitHub PR comment body.
func UnlockResultComment(stackName string, status models.JobStatus, errMsg string) string {
	var b strings.Builder
	writeHeader(&b, "unlock", stackName, status)
	writeError(&b, errMsg)
	return b.String()
}

type contentPiece struct {
	kind string // "error" | "output"
	text string
}

// splitResultComments packs prelude + error + output + footer into as many
// comments as needed so every comment stays under maxCommentRunes. Raw terraform
// text is never discarded — only split across comments.
func splitResultComments(stack, action string, status models.JobStatus, prelude, errMsg, output, footer string) []string {
	errMsg = strings.TrimSpace(errMsg)
	output = strings.TrimSpace(output)

	var pieces []contentPiece
	if errMsg != "" {
		pieces = append(pieces, contentPiece{kind: "error", text: errMsg})
	}
	if output != "" {
		pieces = append(pieces, contentPiece{kind: "output", text: output})
	}

	if len(pieces) == 0 {
		return []string{prelude + footer}
	}

	var comments []string
	part := 1
	var body strings.Builder
	body.WriteString(prelude)
	outputSeen := false

	flush := func() {
		comments = append(comments, body.String())
		body.Reset()
		part++
		writeContinuationHeader(&body, stack, action, part)
	}

	for len(pieces) > 0 {
		piece := pieces[0]
		continued := piece.kind == "output" && outputSeen
		footerIfLast := ""
		if len(pieces) == 1 {
			footerIfLast = footer
		}

		n := maxFittingRunes(body.String(), status, piece.kind, piece.text, continued, footerIfLast)
		if n == 0 {
			// Current comment has no room; start a continuation and retry.
			flush()
			continue
		}

		head, rest := splitRunes(piece.text, n)
		body.WriteString(renderSection(status, piece.kind, head, continued))
		if piece.kind == "output" {
			outputSeen = true
		}

		if rest == "" {
			pieces = pieces[1:]
		} else {
			pieces[0].text = rest
			flush()
		}
	}

	// Footer space was reserved while packing the final piece, so it fits here.
	body.WriteString(footer)
	comments = append(comments, body.String())
	return comments
}

// maxFittingRunes returns the largest prefix of text (in runes) that can be
// rendered into a section appended to prefix (+ optional footer) without
// exceeding maxCommentRunes. Returns 0 if even one rune will not fit.
func maxFittingRunes(prefix string, status models.JobStatus, kind, text string, continued bool, footer string) int {
	runes := []rune(text)
	if len(runes) == 0 {
		return 0
	}

	fits := func(n int) bool {
		rendered := renderSection(status, kind, string(runes[:n]), continued)
		total := prefix + rendered
		if n == len(runes) {
			total += footer
		}
		return utf8.RuneCountInString(total) <= maxCommentRunes
	}

	if fits(len(runes)) {
		return len(runes)
	}
	if !fits(1) {
		return 0
	}

	lo, hi := 1, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func writeContinuationHeader(b *strings.Builder, stack, action string, part int) {
	if stack != "" {
		fmt.Fprintf(b, "### `%s` · %s · output continued (%d)\n", stack, action, part)
		return
	}
	fmt.Fprintf(b, "### %s · output continued (%d)\n", action, part)
}

func renderSection(status models.JobStatus, kind, text string, continued bool) string {
	var b strings.Builder
	switch kind {
	case "error":
		b.WriteString("\n> [!CAUTION]\n")
		b.WriteString("> Something went wrong:\n")
		b.WriteString("\n")
		writeFencedBlock(&b, text)
	case "output":
		if status == models.JobStatusRunning {
			b.WriteString("\n<details open>\n")
			if continued {
				b.WriteString("<summary>Output (live, continued)</summary>\n\n")
			} else {
				b.WriteString("<summary>Output (live)</summary>\n\n")
			}
		} else {
			b.WriteString("\n<details>\n")
			if continued {
				b.WriteString("<summary>Output (continued)</summary>\n\n")
			} else {
				b.WriteString("<summary>Output</summary>\n\n")
			}
		}
		writeFencedBlock(&b, text)
		b.WriteString("</details>\n")
	}
	return b.String()
}

func splitRunes(s string, n int) (head, rest string) {
	if n <= 0 {
		return "", s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s, ""
	}
	return string(runes[:n]), string(runes[n:])
}

func writeHeader(b *strings.Builder, action, stack string, status models.JobStatus) {
	statusLabel := statusLabel(status)
	switch {
	case stack != "":
		fmt.Fprintf(b, "### `%s` · %s · %s\n", stack, action, statusLabel)
	default:
		fmt.Fprintf(b, "### %s · %s\n", action, statusLabel)
	}
}

func writeMeta(b *strings.Builder, job *models.Job) {
	dir := strings.TrimSpace(job.Dir)
	sha := shortSHA(job.CommitSHA)
	switch {
	case dir != "" && sha != "":
		fmt.Fprintf(b, "\n`%s@%s`\n", dir, sha)
	case dir != "":
		fmt.Fprintf(b, "\n`%s`\n", dir)
	case sha != "":
		fmt.Fprintf(b, "\n`%s`\n", sha)
	}
}

// writeDelta renders counts in a ```diff fence so GitHub paints adds green and
// destroys red. A caution callout appears when anything would be destroyed.
func writeDelta(b *strings.Builder, d resourceDelta) {
	b.WriteString("\n```diff\n")
	fmt.Fprintf(b, "+ %d add\n", d.add)
	fmt.Fprintf(b, "  %d change\n", d.change)
	fmt.Fprintf(b, "- %d destroy\n", d.destroy)
	b.WriteString("```\n")

	if d.destroy > 0 {
		b.WriteString("\n> [!CAUTION]\n")
		fmt.Fprintf(b, "> This run destroys **%d** resource", d.destroy)
		if d.destroy != 1 {
			b.WriteString("s")
		}
		b.WriteString(".\n")
	}
}

func writeError(b *strings.Builder, errMsg string) {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return
	}
	b.WriteString("\n> [!CAUTION]\n")
	b.WriteString("> Something went wrong:\n")
	b.WriteString("\n")
	writeFencedBlock(b, errMsg)
}

func statusLabel(status models.JobStatus) string {
	switch status {
	case models.JobStatusSucceeded:
		return "✅ passed"
	case models.JobStatusRunning:
		return "⌛ running"
	default:
		return "❌ failed"
	}
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}

func parsePlanDelta(output string) (resourceDelta, bool) {
	m := planSummaryRE.FindStringSubmatch(output)
	if m == nil {
		return resourceDelta{}, false
	}
	return resourceDelta{
		add:     atoi(m[1]),
		change:  atoi(m[2]),
		destroy: atoi(m[3]),
	}, true
}

func parseApplyDelta(output string) (resourceDelta, bool) {
	m := applySummaryRE.FindStringSubmatch(output)
	if m == nil {
		return resourceDelta{}, false
	}
	return resourceDelta{
		add:     atoi(m[1]),
		change:  atoi(m[2]),
		destroy: atoi(m[3]),
	}, true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// writeFencedBlock writes a fenced code block using a fence long enough to
// avoid colliding with fences inside the body.
func writeFencedBlock(b *strings.Builder, body string) {
	fence := codeFence(body)
	fmt.Fprintf(b, "%s\n%s\n%s\n", fence, body, fence)
}

func codeFence(body string) string {
	max := 3
	inFence := false
	count := 0
	for _, r := range body {
		if r == '`' {
			if !inFence {
				inFence = true
				count = 1
			} else {
				count++
			}
			if count > max {
				max = count
			}
		} else if inFence {
			inFence = false
			count = 0
		}
	}
	return strings.Repeat("`", max+1)
}
