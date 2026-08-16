package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalJobPayload(t *testing.T) {
	got, err := marshalJobPayload(nil)
	require.NoError(t, err)
	require.Equal(t, "{}", got)

	got, err = marshalJobPayload(map[string]interface{}{"dir": "stacks/a", "commit_sha": "abc"})
	require.NoError(t, err)
	require.JSONEq(t, `{"commit_sha":"abc","dir":"stacks/a"}`, got)

	_, err = marshalJobPayload(map[string]interface{}{"bad": make(chan int)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "marshal job payload")
}

func TestStringFromPayload(t *testing.T) {
	require.Equal(t, "", stringFromPayload(nil, "dir"))
	require.Equal(t, "", stringFromPayload(map[string]interface{}{}, "dir"))
	require.Equal(t, "", stringFromPayload(map[string]interface{}{"dir": nil}, "dir"))
	require.Equal(t, "", stringFromPayload(map[string]interface{}{"dir": 12}, "dir"))
	require.Equal(t, "stacks/a", stringFromPayload(map[string]interface{}{"dir": "stacks/a"}, "dir"))
}
