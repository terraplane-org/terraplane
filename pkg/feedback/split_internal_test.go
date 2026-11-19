package feedback

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func TestSplitResultCommentsFlushesWhenPreludeLeavesNoRoom(t *testing.T) {
	prelude := strings.Repeat("P", maxCommentRunes-10)
	parts := splitResultComments("s", "plan", models.JobStatusSucceeded, prelude, "", "hello-output", "\nfooter\n")
	require.GreaterOrEqual(t, len(parts), 2)
	require.Equal(t, prelude, parts[0])
	require.Contains(t, parts[1], "hello-output")
	require.Contains(t, parts[1], "footer")
	for i, part := range parts {
		require.LessOrEqual(t, utf8.RuneCountInString(part), maxCommentRunes, "part %d", i+1)
	}
}

func TestSplitRunesEdgeCases(t *testing.T) {
	head, rest := splitRunes("abc", 0)
	require.Equal(t, "", head)
	require.Equal(t, "abc", rest)

	head, rest = splitRunes("abc", 10)
	require.Equal(t, "abc", head)
	require.Equal(t, "", rest)
}

func TestMaxFittingRunesEmptyText(t *testing.T) {
	require.Equal(t, 0, maxFittingRunes("", models.JobStatusSucceeded, "output", "", false, ""))
}
