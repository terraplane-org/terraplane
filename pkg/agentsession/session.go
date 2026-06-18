package agentsession

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

type Session interface {
	ID() string
	Run(ctx context.Context) error
	Write(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error
}

type session struct {
	id       string
	conn     *websocket.Conn
	logger   log.Logger
	registry Registry
	writeMu  sync.Mutex
}

func (s *session) ID() string {
	return s.id
}

func (s *session) Run(ctx context.Context) error {
	defer func() {
		_ = s.registry.Unregister(ctx, s.id)
		_ = s.conn.Close(websocket.StatusNormalClosure, "")
	}()

	s.logger.Info("Agent session started", "agent_id", s.id)

	for {
		var msg terraplanev1.TerraformEnvelope
		err := wsproto.Read(ctx, s.conn, &msg)
		if err != nil {
			if isExpectedDisconnect(err) || ctx.Err() != nil {
				s.logger.Info("Agent session closed", "agent_id", s.id)
				return nil
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		s.logger.Info("Received websocket message", "agent_id", s.id, "message", msg.String())
	}
}

func (s *session) Write(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return wsproto.Write(ctx, s.conn, msg)
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
