package wsproto

import (
	"context"
	"fmt"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

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
