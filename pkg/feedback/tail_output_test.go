package feedback

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTailOutput(t *testing.T) {
	require.Equal(t, "short", tailOutput("short"))

	got := tailOutput(string([]byte{0xc3, 0xa9}) + strings.Repeat("b", githubOutputMaxBytes-1))
	require.True(t, strings.HasPrefix(got, "...(truncated)\n"))
	require.NotEqual(t, byte(0x80), got[len("...(truncated)\n")]&0xc0)
}
