package feedback

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

var planSummaryRE = regexp.MustCompile(`(?m)^Plan:\s+(\d+)\s+to\s+add,\s+(\d+)\s+to\s+change,\s+(\d+)\s+to\s+destroy\.?$`)

// PlanResultComment formats a plan job result as a GitHub PR comment body.
func PlanResultComment(job *models.Job, success bool, output, errMsg string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "`%s` plan · %s\n", job.StackName, statusEmoji(success))

	if counts := extractPlanCounts(output); counts != "" {
		fmt.Fprintf(&b, "\n%s\n", counts)
	}

	if errMsg = strings.TrimSpace(errMsg); errMsg != "" {
		b.WriteString("\n")
		writeFencedBlock(&b, errMsg)
	}

	if output = strings.TrimSpace(output); output != "" {
		b.WriteString("\n<details>\n")
		b.WriteString("<summary>plan</summary>\n\n")
		writeFencedBlock(&b, output)
		b.WriteString("</details>\n")
	}

	if success {
		fmt.Fprintf(&b, "\napply with `terraplane apply -s %s`\n", job.StackName)
	}

	return b.String()
}

func statusEmoji(success bool) string {
	if success {
		return "✅"
	}
	return "❌"
}

func extractPlanCounts(output string) string {
	m := planSummaryRE.FindStringSubmatch(output)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("%s add · %s change · %s destroy", m[1], m[2], m[3])
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
