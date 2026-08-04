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
	require.NoError(t, wsproto.WriteTerraform(ctx, server, want))

	got, err := wsproto.ReadEnvelope(ctx, client)
	require.NoError(t, err)
	tf := got.GetTerraform()
	require.NotNil(t, tf)
	require.Equal(t, "job-1", tf.GetJobId())
	require.Equal(t, "hello", tf.GetAck().GetMessage())
}

func TestWriteReadPingPong(t *testing.T) {
	server, client := testWSPair(t)
	ctx := context.Background()

	require.NoError(t, wsproto.Write(ctx, server, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Ping{Ping: &terraplanev1.Ping{}},
	}))

	got, err := wsproto.ReadEnvelope(ctx, client)
	require.NoError(t, err)
	require.NotNil(t, got.GetPing())
	require.Nil(t, got.GetTerraform())
}

func TestReadRejectsNonBinaryMessage(t *testing.T) {
	server, client := testWSPair(t)
	ctx := context.Background()

	require.NoError(t, server.Write(ctx, websocket.MessageText, []byte("not-binary")))

	_, err := wsproto.ReadEnvelope(ctx, client)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected binary websocket message")
}

func TestReadUnmarshalFailure(t *testing.T) {
	server, client := testWSPair(t)
	ctx := context.Background()

	require.NoError(t, server.Write(ctx, websocket.MessageBinary, []byte("not-a-proto")))

	_, err := wsproto.ReadEnvelope(ctx, client)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal proto")
}

func TestWrapTerraform(t *testing.T) {
	tf := &terraplanev1.TerraformEnvelope{JobId: "job-1"}
	env := wsproto.WrapTerraform(tf)
	require.Equal(t, "job-1", env.GetTerraform().GetJobId())
}

func TestReadPropagatesConnError(t *testing.T) {
	server, client := testWSPair(t)
	_ = server.CloseNow()

	_, err := wsproto.ReadEnvelope(context.Background(), client)
	require.Error(t, err)
}

func TestWritePropagatesConnError(t *testing.T) {
	server, client := testWSPair(t)
	_ = client.CloseNow()
	_ = server.CloseNow()

	err := wsproto.WriteTerraform(context.Background(), server, &terraplanev1.TerraformEnvelope{JobId: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "write websocket message")
}

func TestConfigureConnAllowsLargeMessage(t *testing.T) {
	server, client := testWSPair(t)
	wsproto.ConfigureConn(server)
	wsproto.ConfigureConn(client)
	ctx := context.Background()

	// Default limit is 32 KiB; send something larger to prove ConfigureConn works.
	big := make([]byte, 64<<10)
	for i := range big {
		big[i] = 'a'
	}
	want := &terraplanev1.TerraformEnvelope{
		JobId: "job-big",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{
				Success: true,
				Output:  string(big),
			},
		},
	}
	require.NoError(t, wsproto.WriteTerraform(ctx, server, want))

	got, err := wsproto.ReadEnvelope(ctx, client)
	require.NoError(t, err)
	require.Equal(t, "job-big", got.GetTerraform().GetJobId())
	require.Len(t, got.GetTerraform().GetPlanResult().GetOutput(), len(big))
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
