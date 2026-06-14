package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
	"google.golang.org/protobuf/proto"
)

type Session struct {
	id     string
	conn   *websocket.Conn
	logger log.Logger
}

func NewSession(id string, conn *websocket.Conn, logger log.Logger) *Session {
	return &Session{
		id:     id,
		conn:   conn,
		logger: logger,
	}
}

func (s *Session) Hello(ctx context.Context) error {
	return wsproto.Write(ctx, s.conn, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Hello{
			Hello: &terraplanev1.Hello{
				AgentId: s.id,
			},
		},
	})
}

func (s *Session) Read(ctx context.Context, msg proto.Message) error {
	return wsproto.Read(ctx, s.conn, msg)
}

func (s *Session) Write(ctx context.Context, msg proto.Message) error {
	return wsproto.Write(ctx, s.conn, msg)
}

func (s *Session) Run(ctx context.Context) error {
	for {
		var msg terraplanev1.TerraformEnvelope
		err := s.Read(ctx, &msg)
		if err != nil {
			if isExpectedDisconnect(err) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		s.logger.Info("Received websocket message", "agent_id", s.id, "message", msg.String())
	}
}

func (s *Session) Close(status websocket.StatusCode, reason string) {
	_ = s.conn.Close(status, reason)
}

func (s *Session) CloseNow() {
	_ = s.conn.CloseNow()
}

func isExpectedDisconnect(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return true
	default:
		return false
	}
}
