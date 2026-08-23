package services

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayloadCommentID(t *testing.T) {
	require.Equal(t, 0, payloadCommentID(nil))
	require.Equal(t, 0, payloadCommentID(map[string]any{}))
	require.Equal(t, 0, payloadCommentID(map[string]any{feedbackCommentIDKey: nil}))
	require.Equal(t, 0, payloadCommentID(map[string]any{feedbackCommentIDKey: "99"}))
	require.Equal(t, 99, payloadCommentID(map[string]any{feedbackCommentIDKey: float64(99)}))
	require.Equal(t, 7, payloadCommentID(map[string]any{feedbackCommentIDKey: 7}))
	require.Equal(t, 8, payloadCommentID(map[string]any{feedbackCommentIDKey: int32(8)}))
	require.Equal(t, 9, payloadCommentID(map[string]any{feedbackCommentIDKey: int64(9)}))
	require.Equal(t, 10, payloadCommentID(map[string]any{feedbackCommentIDKey: json.Number("10")}))
	require.Equal(t, 0, payloadCommentID(map[string]any{feedbackCommentIDKey: json.Number("nope")}))
}
