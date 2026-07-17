package wsproto_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

func TestWriteReadRoundTrip(t *testing.T) {
	server, client := testWSPair(t)
	ctx := context.Background()

	want := &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Ack{
			Ack: &terraplanev1.Ack{Message: "hello"},
		},
	}
	require.NoError(t, wsproto.Write(ctx, server, want))

	var got terraplanev1.TerraformEnvelope
	require.NoError(t, wsproto.Read(ctx, client, &got))
	require.Equal(t, "job-1", got.GetJobId())
	require.Equal(t, "hello", got.GetAck().GetMessage())
}

func TestReadRejectsNonBinaryMessage(t *testing.T) {
	server, client := testWSPair(t)
	ctx := context.Background()

	require.NoError(t, server.Write(ctx, websocket.MessageText, []byte("not-binary")))

	var got terraplanev1.TerraformEnvelope
	err := wsproto.Read(ctx, client, &got)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected binary websocket message")
}

func TestReadUnmarshalFailure(t *testing.T) {
	server, client := testWSPair(t)
	ctx := context.Background()

	require.NoError(t, server.Write(ctx, websocket.MessageBinary, []byte("not-a-proto")))

	var got terraplanev1.TerraformEnvelope
	err := wsproto.Read(ctx, client, &got)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal proto")
}

func TestReadPropagatesConnError(t *testing.T) {
	server, client := testWSPair(t)
	_ = server.CloseNow()

	var got terraplanev1.TerraformEnvelope
	err := wsproto.Read(context.Background(), client, &got)
	require.Error(t, err)
}

func TestWritePropagatesConnError(t *testing.T) {
	server, client := testWSPair(t)
	_ = client.CloseNow()
	_ = server.CloseNow()

	err := wsproto.Write(context.Background(), server, &terraplanev1.TerraformEnvelope{JobId: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "write websocket message")
}

func testWSPair(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()

	ready := make(chan *websocket.Conn, 1)
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		ready <- c
		<-hold
	}))
	t.Cleanup(func() {
		close(hold)
		srv.Close()
	})

	client, _, err := websocket.Dial(context.Background(), srv.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })

	select {
	case server = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket accept")
	}
	t.Cleanup(func() { _ = server.CloseNow() })
	return server, client
}
