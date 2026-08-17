package agentsession

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type stubSession struct {
	id string
}

func (s stubSession) ID() string { return s.id }
func (stubSession) Run(context.Context) error {
	return nil
}
func (stubSession) Write(context.Context, *terraplanev1.TerraformEnvelope) error {
	return nil
}

func TestRegistryRegisterGetUnregister(t *testing.T) {
	reg := NewRegistry(log.Noop())
	ctx := context.Background()

	got, err := reg.Get(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, got)
	require.Empty(t, reg.GetAllAgents())

	sess := stubSession{id: "agent-1"}
	require.NoError(t, reg.Register(ctx, sess))

	got, err = reg.Get(ctx, "agent-1")
	require.NoError(t, err)
	require.Equal(t, "agent-1", got.ID())
	require.Equal(t, []string{"agent-1"}, reg.GetAllAgents())

	require.NoError(t, reg.Unregister(ctx, "agent-1"))
	got, err = reg.Get(ctx, "agent-1")
	require.NoError(t, err)
	require.Nil(t, got)
	require.Empty(t, reg.GetAllAgents())
}

func TestRegistryDuplicateRegisterOverwrites(t *testing.T) {
	// Current contract: last register wins; duplicate is not an error.
	reg := NewRegistry(log.Noop())
	ctx := context.Background()

	first := stubSession{id: "agent-1"}
	second := stubSession{id: "agent-1"}
	require.NoError(t, reg.Register(ctx, first))
	require.NoError(t, reg.Register(ctx, second))

	got, err := reg.Get(ctx, "agent-1")
	require.NoError(t, err)
	require.Equal(t, second, got)
}

func TestRegistryUnregisterMissingIsNoop(t *testing.T) {
	reg := NewRegistry(log.Noop())
	require.NoError(t, reg.Unregister(context.Background(), "never-registered"))
}
