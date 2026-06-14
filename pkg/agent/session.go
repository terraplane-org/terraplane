package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xyzjace/terraplane/pkg/log"
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
	return wsjson.Write(ctx, s.conn, s.id)
}

func (s *Session) Read(ctx context.Context) (string, error) {
	var msg string
	err := wsjson.Read(ctx, s.conn, &msg)
	return msg, err
}

func (s *Session) Write(ctx context.Context, msg any) error {
	return wsjson.Write(ctx, s.conn, msg)
}

func (s *Session) Run(ctx context.Context) error {
	for {
		msg, err := s.Read(ctx)
		if err != nil {
			if isExpectedDisconnect(err) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		s.logger.Info("Received websocket message", "agent_id", s.id, "message", msg)
	}
}

func (s *Session) Close(status websocket.StatusCode, reason string) {
	s.conn.Close(status, reason)
}

func (s *Session) CloseNow() {
	s.conn.CloseNow()
}

func isExpectedDisconnect(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return websocket.CloseStatus(err) == websocket.StatusNormalClosure
}
