package log_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/log"
)

func TestNop_DiscardsAndChains(t *testing.T) {
	logger := log.Nop()
	require.NotNil(t, logger)

	require.NotPanics(t, func() {
		logger.Debug("debug")
		logger.Info("info")
		logger.Warn("warn")
		logger.Error("error")
	})

	child := logger.With("k", "v")
	require.NotNil(t, child)
	require.NotPanics(t, func() {
		child.Info("still quiet")
	})
}
