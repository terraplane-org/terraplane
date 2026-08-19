package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/agentsession/mock_agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type dispatchJobs struct {
	cmd      *command.Command
	err      error
	claimed  []string
	released []string
	failed   []string
	reapErr  error
	reaped   int
}

func (s *dispatchJobs) CreatePendingJobs(context.Context, *scm.Webhook) error { return nil }
func (s *dispatchJobs) ClaimPendingJob(_ context.Context, agentID string) (*command.Command, error) {
	s.claimed = append(s.claimed, agentID)
	return s.cmd, s.err
}
func (s *dispatchJobs) ReleaseClaim(_ context.Context, jobID string) error {
	s.released = append(s.released, jobID)
	return nil
}
func (s *dispatchJobs) FailClaimedJob(_ context.Context, jobID, _ string) error {
	s.failed = append(s.failed, jobID)
	return nil
}
func (s *dispatchJobs) ReapExpiredClaims(context.Context) error {
	s.reaped++
	return s.reapErr
}
func (s *dispatchJobs) RefreshAgentClaims(context.Context, string) error { return nil }
func (s *dispatchJobs) AckJob(context.Context, string) error             { return nil }
func (s *dispatchJobs) CommitJobResult(context.Context, string, string, string, string) error {
	return nil
}

type failingDispatchJobs struct {
	dispatchJobs
	releaseErr error
	failErr    error
}

func (s *failingDispatchJobs) ReleaseClaim(context.Context, string) error {
	return s.releaseErr
}
func (s *failingDispatchJobs) FailClaimedJob(context.Context, string, string) error {
	return s.failErr
}

type dispatchUnlock struct {
	called int
	err    error
}

func (s *dispatchUnlock) RunUnlock(context.Context, command.UnlockCommand) error {
	s.called++
	return s.err
}

type dispatchSession struct {
	id       string
	writeErr error
	wrote    []*terraplanev1.TerraformEnvelope
}

func (s *dispatchSession) ID() string { return s.id }
func (s *dispatchSession) Run(context.Context) error {
	return nil
}
func (s *dispatchSession) Write(_ context.Context, msg *terraplanev1.TerraformEnvelope) error {
	s.wrote = append(s.wrote, msg)
	return s.writeErr
}

func planDispatchCmd() command.Command {
	plan := command.PlanCommand{
		Stacks:    []string{"a"},
		PlanFlags: "-target=x",
	}
	plan.Repo = "acme/infra"
	plan.PRNumber = 42
	plan.CommitSHA = "abc123"
	plan.Agent = "agent-a"
	plan.JobID = "job-1"
	plan.Dir = "stacks/a"
	return command.Command{Kind: command.KindPlan, Plan: plan}
}

func applyDispatchCmd() command.Command {
	apply := command.ApplyCommand{Stacks: []string{"a"}}
	apply.Repo = "acme/infra"
	apply.PRNumber = 42
	apply.CommitSHA = "abc123"
	apply.Agent = "agent-a"
	apply.JobID = "job-2"
	apply.Dir = "stacks/a"
	return command.Command{Kind: command.KindApply, Apply: apply}
}

func unlockDispatchCmd() command.Command {
	unlock := command.UnlockCommand{Stacks: []string{"a"}}
	unlock.Repo = "acme/infra"
	unlock.PRNumber = 42
	unlock.Agent = "agent-a"
	unlock.JobID = "job-3"
	return command.Command{Kind: command.KindUnlock, Unlock: unlock}
}

func startAndCancel(t *testing.T, d Dispatcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	require.NoError(t, d.Shutdown(context.Background()))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}

func TestNewDispatcherStartShutdown(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().GetAllAgents().Return(nil).AnyTimes()

	jobs := &dispatchJobs{}
	d := NewDispatcher(
		&config.Config{OrchestratorDispatcherJobPollInterval: time.Millisecond},
		log.Noop(),
		jobs,
		reg,
		&dispatchUnlock{},
	)

	startAndCancel(t, d)
	require.GreaterOrEqual(t, jobs.reaped, 1)
}

func TestPollClaimErrorIsLogged(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().GetAllAgents().Return([]string{"agent-a"}).AnyTimes()

	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{err: errors.New("db down")},
		sessionRegistry: reg,
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
}

func TestPollReapErrorStillClaims(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().GetAllAgents().Return(nil).AnyTimes()

	jobs := &dispatchJobs{reapErr: errors.New("reap failed")}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      jobs,
		sessionRegistry: reg,
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
	require.GreaterOrEqual(t, jobs.reaped, 1)
}

func TestDispatchJobPlanWritesEnvelope(t *testing.T) {
	session := &dispatchSession{id: "agent-a"}
	jobs := &dispatchJobs{}
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), session))

	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      jobs,
		sessionRegistry: reg,
	}

	require.NoError(t, d.dispatchJob(context.Background(), planDispatchCmd()))
	require.Len(t, session.wrote, 1)
	require.Equal(t, "job-1", session.wrote[0].GetJobId())
	require.Equal(t, "a", session.wrote[0].GetPlan().GetStackName())
	require.Equal(t, "stacks/a", session.wrote[0].GetPlan().GetDir())
	require.Equal(t, "-target=x", session.wrote[0].GetPlan().GetPlanFlags())
	require.Empty(t, jobs.released)
	require.Empty(t, jobs.failed)
}

func TestDispatchJobApplyWritesEnvelope(t *testing.T) {
	session := &dispatchSession{id: "agent-a"}
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), session))

	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{},
		sessionRegistry: reg,
	}

	require.NoError(t, d.dispatchJob(context.Background(), applyDispatchCmd()))
	require.Len(t, session.wrote, 1)
	require.Equal(t, "job-2", session.wrote[0].GetJobId())
	require.Equal(t, "a", session.wrote[0].GetApply().GetStackName())
	require.Equal(t, "stacks/a", session.wrote[0].GetApply().GetDir())
}

func TestDispatchJobUnlockDoesNotNeedSession(t *testing.T) {
	unlock := &dispatchUnlock{}
	d := &dispatcher{
		logger:        log.Noop(),
		jobService:    &dispatchJobs{},
		unlockService: unlock,
	}

	require.NoError(t, d.dispatchJob(context.Background(), unlockDispatchCmd()))
	require.Equal(t, 1, unlock.called)
}

func TestDispatchJobUnlockErrorMarksFailed(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{
		logger:        log.Noop(),
		jobService:    jobs,
		unlockService: &dispatchUnlock{err: errors.New("unlock failed")},
	}

	err := d.dispatchJob(context.Background(), unlockDispatchCmd())
	require.Error(t, err)
	require.Equal(t, []string{"job-3"}, jobs.failed)
}

func TestDispatchJobUnknownKind(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{logger: log.Noop(), jobService: jobs}
	err := d.dispatchJob(context.Background(), command.Command{Kind: command.Kind("weird")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command kind")
	require.Empty(t, jobs.failed)
}

func TestDispatchJobUnknownKindWithJobIDMarksFailed(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{logger: log.Noop(), jobService: jobs}
	cmd := command.Command{Kind: command.Kind("weird")}
	cmd.Plan.JobID = "job-9"
	err := d.dispatchJob(context.Background(), cmd)
	require.Error(t, err)
	require.Empty(t, jobs.failed)
}

func TestDispatchJobPlanMissingStackMarksFailed(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{logger: log.Noop(), jobService: jobs}
	cmd := planDispatchCmd()
	cmd.Plan.Stacks = nil

	err := d.dispatchJob(context.Background(), cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing job id or stack")
	require.Equal(t, []string{"job-1"}, jobs.failed)
}

func TestDispatchJobApplyMissingJobIDMarksFailed(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{logger: log.Noop(), jobService: jobs}
	cmd := applyDispatchCmd()
	cmd.Apply.JobID = ""

	err := d.dispatchJob(context.Background(), cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing job id or stack")
	require.Empty(t, jobs.failed)
}

func TestDispatchJobSessionLookupErrorReleasesClaim(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().Get(gomock.Any(), "agent-a").Return(nil, errors.New("registry down"))

	jobs := &dispatchJobs{}
	d := &dispatcher{logger: log.Noop(), jobService: jobs, sessionRegistry: reg}

	err := d.dispatchJob(context.Background(), planDispatchCmd())
	require.Error(t, err)
	require.Contains(t, err.Error(), "registry down")
	require.Equal(t, []string{"job-1"}, jobs.released)
}

func TestDispatchJobAgentNotConnectedReleasesClaim(t *testing.T) {
	jobs := &dispatchJobs{}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      jobs,
		sessionRegistry: agentsession.NewRegistry(log.Noop()),
	}

	err := d.dispatchJob(context.Background(), planDispatchCmd())
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not connected")
	require.Equal(t, []string{"job-1"}, jobs.released)
}

func TestDispatchJobWriteErrorReleasesClaim(t *testing.T) {
	session := &dispatchSession{id: "agent-a", writeErr: errors.New("agent gone")}
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), session))

	jobs := &dispatchJobs{}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      jobs,
		sessionRegistry: reg,
	}

	err := d.dispatchJob(context.Background(), planDispatchCmd())
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent gone")
	require.Equal(t, []string{"job-1"}, jobs.released)
}

func TestReleaseAndFailClaimLogRepositoryErrors(t *testing.T) {
	jobs := &failingDispatchJobs{
		releaseErr: errors.New("release failed"),
		failErr:    errors.New("fail failed"),
	}
	d := &dispatcher{logger: log.Noop(), jobService: jobs}

	d.releaseClaim(context.Background(), "job-1")
	d.failClaim(context.Background(), "job-1", "nope")
	d.releaseClaim(context.Background(), "")
	d.failClaim(context.Background(), "", "nope")
}

func TestPollDispatchesClaimedJobs(t *testing.T) {
	session := &dispatchSession{id: "agent-a"}
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), session))

	plan := planDispatchCmd()
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{cmd: &plan},
		sessionRegistry: reg,
		jobPollInterval: time.Millisecond,
	}

	startAndCancel(t, d)
	require.NotEmpty(t, session.wrote)
}

func TestPollDispatchErrorIsLogged(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := mock_agentsession.NewMockRegistry(ctrl)
	reg.EXPECT().GetAllAgents().Return([]string{"agent-a"}).AnyTimes()

	weird := command.Command{Kind: command.Kind("weird")}
	d := &dispatcher{
		logger:          log.Noop(),
		jobService:      &dispatchJobs{cmd: &weird},
		sessionRegistry: reg,
		jobPollInterval: time.Millisecond,
	}
	startAndCancel(t, d)
}

func TestTickClaimsUnlockThenEachAgent(t *testing.T) {
	reg := agentsession.NewRegistry(log.Noop())
	require.NoError(t, reg.Register(context.Background(), &dispatchSession{id: "agent-a"}))
	require.NoError(t, reg.Register(context.Background(), &dispatchSession{id: "agent-b"}))

	jobs := &dispatchJobs{}
	d := &dispatcher{logger: log.Noop(), jobService: jobs, sessionRegistry: reg}
	d.tick(context.Background())

	require.Equal(t, "", jobs.claimed[0])
	require.ElementsMatch(t, []string{"agent-a", "agent-b"}, jobs.claimed[1:])
}

func TestCommandHelpers(t *testing.T) {
	require.Equal(t, "job-1", commandJobID(planDispatchCmd()))
	require.Equal(t, "job-2", commandJobID(applyDispatchCmd()))
	require.Equal(t, "job-3", commandJobID(unlockDispatchCmd()))
	require.Equal(t, "", commandJobID(command.Command{}))

	require.Equal(t, "agent-a", commandAgent(planDispatchCmd()))
	require.Equal(t, "agent-a", commandAgent(applyDispatchCmd()))
	require.Equal(t, "agent-a", commandAgent(unlockDispatchCmd()))
	require.Equal(t, "", commandAgent(command.Command{}))

	_, err := terraformEnvelope(unlockDispatchCmd())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command kind")
}
