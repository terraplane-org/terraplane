package agentsession

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xyzjace/terraplane/pkg/log"
)

type Session interface {
	ID() string
	Run(ctx context.Context) error
}

type session struct {
	id       string
	conn     *websocket.Conn
	logger   log.Logger
	registry Registry
}

func (s *session) ID() string {
	return s.id
}

func (s *session) Run(ctx context.Context) error {
	defer s.registry.Unregister(ctx, s.id)
	defer s.conn.Close(websocket.StatusNormalClosure, "")

	s.logger.Info("Agent session started", "agent_id", s.id)

	for {
		var msg string
		err := wsjson.Read(ctx, s.conn, &msg)
		if err != nil {
			if isExpectedDisconnect(err) || ctx.Err() != nil {
				s.logger.Info("Agent session closed", "agent_id", s.id)
				return nil
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		s.logger.Info("Received websocket message", "agent_id", s.id, "message", msg)
	}
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
