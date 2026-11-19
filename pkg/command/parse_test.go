package command

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJobID(t *testing.T) {
	plan := &Command{Kind: KindPlan}
	plan.Plan.JobID = "plan-job"
	require.Equal(t, "plan-job", plan.JobID())

	apply := &Command{Kind: KindApply}
	apply.Apply.JobID = "apply-job"
	require.Equal(t, "apply-job", apply.JobID())

	unlock := &Command{Kind: KindUnlock}
	unlock.Unlock.JobID = "unlock-job"
	require.Equal(t, "unlock-job", unlock.JobID())

	unknown := &Command{Kind: KindUnknown}
	require.Equal(t, "", unknown.JobID())
}

func TestVerbGuardsNonTerraplaneInput(t *testing.T) {
	require.Equal(t, KindUnknown, verb(nil))
	require.Equal(t, KindUnknown, verb([]string{"hello"}))
	require.Equal(t, KindUnknown, verb([]string{"Nope", "plan"}))
	require.Equal(t, KindPlan, verb([]string{"terraplane", "plan"}))
}
