package wsproto

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"google.golang.org/protobuf/proto"
)

// MaxMessageBytes is the per-message WebSocket read limit (uncompressed).
// coder/websocket defaults to 32 KiB, which is far too small for Terraform
// plan/apply output returned as a single protobuf payload.
const MaxMessageBytes = 16 << 20 // 16 MiB

func ConfigureConn(conn *websocket.Conn) {
	conn.SetReadLimit(MaxMessageBytes)
}

func AcceptOptions() *websocket.AcceptOptions {
	return &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	}
}

func DialOptions(header http.Header) *websocket.DialOptions {
	return &websocket.DialOptions{
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionContextTakeover,
	}
}

func Write(ctx context.Context, conn *websocket.Conn, msg proto.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal proto: %w", err)
	}

	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		return fmt.Errorf("write websocket message: %w", err)
	}

	return nil
}

// WriteTerraform wraps a TerraformEnvelope in WebsocketEnvelope and writes it.
func WriteTerraform(ctx context.Context, conn *websocket.Conn, tf *terraplanev1.TerraformEnvelope) error {
	return Write(ctx, conn, WrapTerraform(tf))
}

func WrapTerraform(tf *terraplanev1.TerraformEnvelope) *terraplanev1.WebsocketEnvelope {
	return &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Terraform{Terraform: tf},
	}
}

func Read(ctx context.Context, conn *websocket.Conn, msg proto.Message) error {
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}

	if typ != websocket.MessageBinary {
		return fmt.Errorf("expected binary websocket message, got %v", typ)
	}

	if err := proto.Unmarshal(data, msg); err != nil {
		return fmt.Errorf("unmarshal proto: %w", err)
	}

	return nil
}

func ReadEnvelope(ctx context.Context, conn *websocket.Conn) (*terraplanev1.WebsocketEnvelope, error) {
	var env terraplanev1.WebsocketEnvelope
	if err := Read(ctx, conn, &env); err != nil {
		return nil, err
	}
	return &env, nil
}
