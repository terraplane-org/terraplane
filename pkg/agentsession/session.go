package agentsession

import (
	"context"
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
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				s.logger.Info("Agent session closed", "agent_id", s.id)
				return nil
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		s.logger.Info("Received websocket message", "agent_id", s.id, "message", msg)
	}
}
