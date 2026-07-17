package log_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xyzjace/terraplane/pkg/log"
)

func TestNoop(t *testing.T) {
	logger := log.Noop()
	require.NotNil(t, logger)

	require.NotPanics(t, func() {
		logger.Debug("debug", "k", 1)
		logger.Info("info")
		logger.Warn("warn")
		logger.Error("error", "err", "x")
	})

	child := logger.With("k", "v")
	require.NotNil(t, child)
	require.NotPanics(t, func() {
		child.Debug("d")
		child.Info("i")
		child.Warn("w")
		child.Error("e")
		grandchild := child.With("nested", true)
		grandchild.Info("still quiet")
	})
}
