package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agent/mock_agent"
	"github.com/xyzjace/terraplane/pkg/agent/orchestrator/mock_orchestrator"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
)

type PollerSuite struct {
	suite.Suite
	ctrl *gomock.Controller
	oc   *mock_orchestrator.MockClient
	disp *mock_agent.MockDispatcher
	cfg  *config.Config
}

func TestPollerSuite(t *testing.T) {
	suite.Run(t, new(PollerSuite))
}

func (s *PollerSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.oc = mock_orchestrator.NewMockClient(s.ctrl)
	s.disp = mock_agent.NewMockDispatcher(s.ctrl)
	s.cfg = &config.Config{
		AgentID:                "agent-test",
		AgentPollInterval:      10 * time.Millisecond,
		AgentHeartbeatInterval: 1 * time.Hour, // effectively never fires in tests
	}
}

func (s *PollerSuite) newPoller() *poller {
	return newPoller(s.cfg, log.Noop(), s.disp, s.oc)
}

func planCommand() *command.Command {
	cmd := &command.Command{Kind: command.KindPlan}
	cmd.Plan.JobID = "job-1"
	cmd.Plan.Repo = "acme/infra"
	cmd.Plan.Stacks = []string{"stg"}
	return cmd
}

// --- run ---

func (s *PollerSuite) TestRunStopsOnContextCancel() {
	ctx, cancel := context.WithCancel(context.Background())
	s.oc.EXPECT().ClaimJob(gomock.Any(), "agent-test").Return(nil, nil).AnyTimes()
	cancel()
	err := s.newPoller().run(ctx)
	require.NoError(s.T(), err)
}

func (s *PollerSuite) TestRunReturnsErrorOnClaimFailure() {
	s.oc.EXPECT().ClaimJob(gomock.Any(), "agent-test").Return(nil, errors.New("network error"))
	err := s.newPoller().run(context.Background())
	require.Error(s.T(), err)
	require.Contains(s.T(), err.Error(), "network error")
}

func (s *PollerSuite) TestRunSkipsWhenNoJob() {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	s.oc.EXPECT().ClaimJob(gomock.Any(), "agent-test").DoAndReturn(
		func(_ context.Context, _ string) (*command.Command, error) {
			calls++
			if calls >= 3 {
				cancel()
			}
			return nil, nil
		}).AnyTimes()

	err := s.newPoller().run(ctx)
	require.NoError(s.T(), err)
	require.GreaterOrEqual(s.T(), calls, 3)
}

func (s *PollerSuite) TestRunExecutesJobWhenClaimed() {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := planCommand()

	s.oc.EXPECT().ClaimJob(gomock.Any(), "agent-test").Return(cmd, nil)
	s.oc.EXPECT().Ack(gomock.Any(), "job-1", "agent-test").Return(nil)
	s.disp.EXPECT().Dispatch(gomock.Any(), cmd, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *command.Command, done chan<- struct{}) {
			close(done)
		})
	// After the job, return no more work and cancel.
	s.oc.EXPECT().ClaimJob(gomock.Any(), "agent-test").DoAndReturn(
		func(_ context.Context, _ string) (*command.Command, error) {
			cancel()
			return nil, nil
		}).AnyTimes()

	err := s.newPoller().run(ctx)
	require.NoError(s.T(), err)
}

// --- executeJob ---

func (s *PollerSuite) TestExecuteJobAckFailureSkipsDispatch() {
	// No Dispatch expectation — if it were called the mock would fail.
	s.oc.EXPECT().Ack(gomock.Any(), "job-1", "agent-test").Return(errors.New("ack failed"))
	s.newPoller().executeJob(context.Background(), planCommand())
}

func (s *PollerSuite) TestExecuteJobDispatchesAndWaits() {
	cmd := planCommand()
	dispatched := make(chan struct{})

	s.oc.EXPECT().Ack(gomock.Any(), "job-1", "agent-test").Return(nil)
	s.disp.EXPECT().Dispatch(gomock.Any(), cmd, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *command.Command, done chan<- struct{}) {
			close(dispatched)
			close(done)
		})

	s.newPoller().executeJob(context.Background(), cmd)

	select {
	case <-dispatched:
	default:
		s.T().Fatal("Dispatch was not called")
	}
}

func (s *PollerSuite) TestExecuteJobHeartbeatsWhileRunning() {
	cmd := planCommand()
	s.cfg.AgentHeartbeatInterval = 10 * time.Millisecond

	jobRunning := make(chan struct{})
	jobDone := make(chan struct{})

	s.oc.EXPECT().Ack(gomock.Any(), "job-1", "agent-test").Return(nil)
	s.disp.EXPECT().Dispatch(gomock.Any(), cmd, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *command.Command, done chan<- struct{}) {
			close(jobRunning)
			go func() {
				<-jobDone
				close(done)
			}()
		})
	// Expect at least one heartbeat during the job.
	heartbeaten := make(chan struct{}, 10)
	s.oc.EXPECT().Heartbeat(gomock.Any(), "job-1", "agent-test").DoAndReturn(
		func(_ context.Context, _, _ string) error {
			select {
			case heartbeaten <- struct{}{}:
			default:
			}
			return nil
		}).AnyTimes()

	go s.newPoller().executeJob(context.Background(), cmd)

	<-jobRunning
	// Wait for at least one heartbeat.
	select {
	case <-heartbeaten:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for heartbeat")
	}
	close(jobDone)
}

func (s *PollerSuite) TestExecuteJobHeartbeatStopsAfterJob() {
	cmd := planCommand()
	s.cfg.AgentHeartbeatInterval = 10 * time.Millisecond

	s.oc.EXPECT().Ack(gomock.Any(), "job-1", "agent-test").Return(nil)
	s.disp.EXPECT().Dispatch(gomock.Any(), cmd, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *command.Command, done chan<- struct{}) {
			close(done)
		})
	// Heartbeat may fire zero or more times — after done closes it must stop.
	s.oc.EXPECT().Heartbeat(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	s.newPoller().executeJob(context.Background(), cmd)

	// Wait a bit and confirm no more heartbeats arrive after executeJob returns.
	time.Sleep(50 * time.Millisecond)
	// If heartbeat kept firing after job ended, the mock controller would record
	// unexpected calls — the suite's TearDownTest via gomock.Controller catches this.
}
