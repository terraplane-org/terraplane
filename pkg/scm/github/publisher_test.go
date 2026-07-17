package github

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm/github/mock_github"
)

func TestPublisherName(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := mock_github.NewMockClient(ctrl)
	pub := NewPublisher(log.Noop(), client)
	require.Equal(t, "github", pub.Name())
}

func TestPublisherWriteCommentSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := mock_github.NewMockClient(ctrl)
	client.EXPECT().WriteComment(gomock.Any(), "acme/infra", 3, "hi").Return(nil)

	pub := NewPublisher(log.Noop(), client)
	require.NoError(t, pub.WriteComment(context.Background(), "acme/infra", 3, "hi"))
}

func TestPublisherWriteCommentPropagatesError(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := mock_github.NewMockClient(ctrl)
	client.EXPECT().WriteComment(gomock.Any(), "acme/infra", 3, "hi").Return(errors.New("api down"))

	pub := NewPublisher(log.Noop(), client)
	err := pub.WriteComment(context.Background(), "acme/infra", 3, "hi")
	require.Error(t, err)
	require.Contains(t, err.Error(), "api down")
}
