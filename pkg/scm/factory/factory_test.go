package factory_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm/factory"
)

func TestNewProviderGitHub(t *testing.T) {
	provider, err := factory.NewProvider(log.Noop(), &config.Config{SCMProvider: "github"})
	require.NoError(t, err)
	require.Equal(t, "github", provider.Name())
}

func TestNewProviderDefault(t *testing.T) {
	provider, err := factory.NewProvider(log.Noop(), &config.Config{})
	require.NoError(t, err)
	require.Equal(t, "github", provider.Name())
}

func TestNewProviderUnknown(t *testing.T) {
	_, err := factory.NewProvider(log.Noop(), &config.Config{SCMProvider: "bitbucket"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown SCM provider")
}

func TestNewPublisherGitHub(t *testing.T) {
	publisher, err := factory.NewPublisher(log.Noop(), &config.Config{SCMProvider: "github"})
	require.NoError(t, err)
	require.Equal(t, "github", publisher.Name())
}

func TestNewPublisherUnknown(t *testing.T) {
	_, err := factory.NewPublisher(log.Noop(), &config.Config{SCMProvider: "gitlab"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown SCM provider")
}
