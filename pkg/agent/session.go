package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/handlers"
	"github.com/xyzjace/terraplane/pkg/agent/orchestrator"
	"github.com/xyzjace/terraplane/pkg/agent/terraform"
	"github.com/xyzjace/terraplane/pkg/agent/workspace"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
	"google.golang.org/protobuf/proto"
)

type Session struct {
	id       string
	conn     *websocket.Conn
	logger   log.Logger
	handlers *handlers.Handlers
	writeMu  sync.Mutex
}

func NewSession(
	id string,
	conn *websocket.Conn,
	logger log.Logger,
	cfg *config.Config,
	workspaceManager workspace.Manager,
	terraformManager terraform.Manager,
	orchestratorClient orchestrator.Client,
) *Session {
	s := &Session{
		id:     id,
		conn:   conn,
		logger: logger,
	}
	s.handlers = handlers.New(logger, cfg, workspaceManager, terraformManager, orchestratorClient)
	return s
}

func (s *Session) Hello(ctx context.Context) error {
	return s.write(ctx, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Hello{
			Hello: &terraplanev1.Hello{
				AgentId: s.id,
			},
		},
	})
}

func (s *Session) write(ctx context.Context, msg proto.Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return wsproto.Write(ctx, s.conn, msg)
}

func (s *Session) WriteTerraform(ctx context.Context, msg *terraplanev1.TerraformEnvelope) error {
	return s.write(ctx, wsproto.WrapTerraform(msg))
}

func (s *Session) Run(ctx context.Context) error {
	for {
		env, err := wsproto.ReadEnvelope(ctx, s.conn)
		if err != nil {
			if isExpectedDisconnect(err) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read websocket message: %w", err)
		}

		switch env.GetPayload().(type) {
		case *terraplanev1.WebsocketEnvelope_Ping:
			s.logger.Debug("Received orchestrator ping", "agent_id", s.id)
			if err := s.write(ctx, &terraplanev1.WebsocketEnvelope{
				Payload: &terraplanev1.WebsocketEnvelope_Pong{Pong: &terraplanev1.Pong{}},
			}); err != nil {
				if isExpectedDisconnect(err) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("write pong: %w", err)
			}
			s.logger.Debug("Sent pong", "agent_id", s.id)
		case *terraplanev1.WebsocketEnvelope_Pong:
			s.logger.Debug("Ignoring unexpected pong from orchestrator", "agent_id", s.id)
		case *terraplanev1.WebsocketEnvelope_Terraform:
			tf := env.GetTerraform()
			if tf == nil {
				s.logger.Debug("Ignoring empty terraform payload", "agent_id", s.id)
				continue
			}
			tf = proto.Clone(tf).(*terraplanev1.TerraformEnvelope)
			cmd := protoEnvelopeToCommand(tf)
			if cmd == nil {
				s.logger.Warn("Ignoring unrecognised terraform envelope payload", "agent_id", s.id, "job_id", tf.GetJobId())
				continue
			}
			s.handlers.Dispatch(ctx, cmd)
		default:
			s.logger.Debug("Ignoring unexpected websocket payload", "agent_id", s.id, "payload", fmt.Sprintf("%T", env.GetPayload()))
		}
	}
}

func (s *Session) Close(status websocket.StatusCode, reason string) {
	_ = s.conn.Close(status, reason)
}

func (s *Session) CloseNow() {
	_ = s.conn.CloseNow()
}

// protoEnvelopeToCommand converts a WS proto envelope into the command type used by handlers.
// This is temporary bridge code that will be removed when the WS path is deleted in PR 8.
func protoEnvelopeToCommand(env *terraplanev1.TerraformEnvelope) *command.Command {
	jobID := env.GetJobId()
	switch p := env.GetPayload().(type) {
	case *terraplanev1.TerraformEnvelope_Plan:
		cmd := &command.Command{Kind: command.KindPlan}
		cmd.Plan.JobID = jobID
		cmd.Plan.Repo = p.Plan.GetRepo()
		cmd.Plan.CommitSHA = p.Plan.GetCommitHash()
		cmd.Plan.PRNumber = int(p.Plan.GetPrNumber())
		cmd.Plan.Dir = p.Plan.GetDir()
		cmd.Plan.PlanFlags = p.Plan.GetPlanFlags()
		cmd.Plan.Stacks = []string{p.Plan.GetStackName()}
		return cmd
	case *terraplanev1.TerraformEnvelope_Apply:
		cmd := &command.Command{Kind: command.KindApply}
		cmd.Apply.JobID = jobID
		cmd.Apply.Repo = p.Apply.GetRepo()
		cmd.Apply.CommitSHA = p.Apply.GetCommitHash()
		cmd.Apply.PRNumber = int(p.Apply.GetPrNumber())
		cmd.Apply.Dir = p.Apply.GetDir()
		cmd.Apply.Stacks = []string{p.Apply.GetStackName()}
		return cmd
	case *terraplanev1.TerraformEnvelope_Unlock:
		cmd := &command.Command{Kind: command.KindUnlock}
		cmd.Unlock.JobID = jobID
		cmd.Unlock.Repo = p.Unlock.GetRepo()
		cmd.Unlock.PRNumber = int(p.Unlock.GetPrNumber())
		return cmd
	default:
		return nil
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
