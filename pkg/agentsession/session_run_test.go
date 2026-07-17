package agentsession

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/storage/repository/mock_repository"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

func TestIsExpectedDisconnect(t *testing.T) {
	require.True(t, isExpectedDisconnect(context.Canceled))
	require.True(t, isExpectedDisconnect(context.DeadlineExceeded))

	require.True(t, isExpectedDisconnect(websocket.CloseError{Code: websocket.StatusNormalClosure}))
	require.True(t, isExpectedDisconnect(websocket.CloseError{Code: websocket.StatusGoingAway}))
	require.False(t, isExpectedDisconnect(websocket.CloseError{Code: websocket.StatusInternalError}))
	require.False(t, isExpectedDisconnect(errors.New("boom")))
}

func TestFactoryNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := NewRegistry(log.Noop())
	jobs := mock_repository.NewMockJobRepository(ctrl)
	locks := mock_repository.NewMockLockRepository(ctrl)
	pub := mock_scm.NewMockPublisher(ctrl)

	f := NewFactory(log.Noop(), reg, jobs, locks, pub)
	sess := f.New("agent-x", nil)
	require.Equal(t, "agent-x", sess.ID())
}

func TestSessionRunHandlesAckThenCloses(t *testing.T) {
	ctrl := gomock.NewController(t)
	jobs := mock_repository.NewMockJobRepository(ctrl)
	locks := mock_repository.NewMockLockRepository(ctrl)
	pub := mock_scm.NewMockPublisher(ctrl)
	reg := NewRegistry(log.Noop())

	serverConn, clientConn := testWSPair(t)

	sess := NewFactory(log.Noop(), reg, jobs, locks, pub).New("agent-1", serverConn)
	require.NoError(t, reg.Register(context.Background(), sess))

	processed := make(chan struct{})
	job := &models.Job{ID: "job-1", Status: models.JobStatusPending, Repo: "acme/infra", PRNumber: 1, StackName: "a"}
	jobs.EXPECT().Get(gomock.Any(), "job-1").Return(job, nil)
	jobs.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&models.Job{})).DoAndReturn(
		func(_ context.Context, updated *models.Job) error {
			require.Equal(t, models.JobStatusRunning, updated.Status)
			close(processed)
			return nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Run(ctx) }()

	require.NoError(t, wsproto.Write(ctx, clientConn, &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Ack{Ack: &terraplanev1.Ack{Message: "ok"}},
	}))

	select {
	case <-processed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ack handling")
	}
	cancel()
	// Complete the peer side of the close handshake so Run's clean Close doesn't wait.
	_ = clientConn.CloseNow()
	require.NoError(t, <-done)

	got, err := reg.Get(context.Background(), "agent-1")
	require.NoError(t, err)
	require.Nil(t, got, "Run must unregister the session on exit")
}

func TestSessionRunPlanAndApplyResults(t *testing.T) {
	ctrl := gomock.NewController(t)
	jobs := mock_repository.NewMockJobRepository(ctrl)
	locks := mock_repository.NewMockLockRepository(ctrl)
	pub := mock_scm.NewMockPublisher(ctrl)
	reg := NewRegistry(log.Noop())

	serverConn, clientConn := testWSPair(t)
	sess := NewFactory(log.Noop(), reg, jobs, locks, pub).New("agent-1", serverConn)
	require.NoError(t, reg.Register(context.Background(), sess))

	planJob := &models.Job{ID: "plan-1", Repo: "acme/infra", PRNumber: 2, StackName: "a", Status: models.JobStatusRunning}
	applyJob := &models.Job{ID: "apply-1", Repo: "acme/infra", PRNumber: 2, StackName: "a", Status: models.JobStatusRunning}
	processed := make(chan struct{})

	jobs.EXPECT().Get(gomock.Any(), "plan-1").Return(planJob, nil)
	jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	pub.EXPECT().WriteComment(gomock.Any(), "acme/infra", 2, gomock.Any()).Return(nil)

	jobs.EXPECT().Get(gomock.Any(), "apply-1").Return(applyJob, nil)
	jobs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	locks.EXPECT().Delete(gomock.Any(), "acme/infra", "a", "default").Return(nil)
	pub.EXPECT().WriteComment(gomock.Any(), "acme/infra", 2, gomock.Any()).DoAndReturn(
		func(context.Context, string, int, string) error {
			close(processed)
			return nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Run(ctx) }()

	require.NoError(t, wsproto.Write(ctx, clientConn, &terraplanev1.TerraformEnvelope{
		JobId: "plan-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true, Output: "ok"},
		},
	}))
	require.NoError(t, wsproto.Write(ctx, clientConn, &terraplanev1.TerraformEnvelope{
		JobId: "apply-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true, Output: "ok"},
		},
	}))

	select {
	case <-processed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for apply result handling")
	}
	cancel()
	_ = clientConn.CloseNow()
	require.NoError(t, <-done)
}

func TestSessionRunHandlerError(t *testing.T) {
	ctrl := gomock.NewController(t)
	jobs := mock_repository.NewMockJobRepository(ctrl)
	locks := mock_repository.NewMockLockRepository(ctrl)
	pub := mock_scm.NewMockPublisher(ctrl)
	reg := NewRegistry(log.Noop())

	serverConn, clientConn := testWSPair(t)
	sess := NewFactory(log.Noop(), reg, jobs, locks, pub).New("agent-1", serverConn)
	require.NoError(t, reg.Register(context.Background(), sess))

	jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))

	done := make(chan error, 1)
	go func() { done <- sess.Run(context.Background()) }()

	require.NoError(t, wsproto.Write(context.Background(), clientConn, &terraplanev1.TerraformEnvelope{
		JobId:   "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Ack{Ack: &terraplanev1.Ack{}},
	}))

	err := <-done
	require.Error(t, err)
	require.Contains(t, err.Error(), "error handling ack")
}

func TestSessionRunPlanResultHandlerError(t *testing.T) {
	ctrl := gomock.NewController(t)
	jobs := mock_repository.NewMockJobRepository(ctrl)
	reg := NewRegistry(log.Noop())
	serverConn, clientConn := testWSPair(t)
	sess := NewFactory(log.Noop(), reg, jobs, mock_repository.NewMockLockRepository(ctrl), mock_scm.NewMockPublisher(ctrl)).
		New("agent-1", serverConn)

	jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))

	done := make(chan error, 1)
	go func() { done <- sess.Run(context.Background()) }()

	require.NoError(t, wsproto.Write(context.Background(), clientConn, &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_PlanResult{
			PlanResult: &terraplanev1.PlanResult{Success: true},
		},
	}))

	err := <-done
	require.Error(t, err)
	require.Contains(t, err.Error(), "error handling plan result")
}

func TestSessionRunApplyResultHandlerError(t *testing.T) {
	ctrl := gomock.NewController(t)
	jobs := mock_repository.NewMockJobRepository(ctrl)
	reg := NewRegistry(log.Noop())
	serverConn, clientConn := testWSPair(t)
	sess := NewFactory(log.Noop(), reg, jobs, mock_repository.NewMockLockRepository(ctrl), mock_scm.NewMockPublisher(ctrl)).
		New("agent-1", serverConn)

	jobs.EXPECT().Get(gomock.Any(), "job-1").Return(nil, errors.New("db"))

	done := make(chan error, 1)
	go func() { done <- sess.Run(context.Background()) }()

	require.NoError(t, wsproto.Write(context.Background(), clientConn, &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_ApplyResult{
			ApplyResult: &terraplanev1.ApplyResult{Success: true},
		},
	}))

	err := <-done
	require.Error(t, err)
	require.Contains(t, err.Error(), "error handling apply result")
}

func TestSessionRunUnexpectedDisconnect(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := NewRegistry(log.Noop())
	serverConn, clientConn := testWSPair(t)
	sess := NewFactory(
		log.Noop(),
		reg,
		mock_repository.NewMockJobRepository(ctrl),
		mock_repository.NewMockLockRepository(ctrl),
		mock_scm.NewMockPublisher(ctrl),
	).New("agent-1", serverConn)
	require.NoError(t, reg.Register(context.Background(), sess))

	done := make(chan error, 1)
	go func() { done <- sess.Run(context.Background()) }()

	// CloseNow avoids the close-handshake timeout that StatusInternalError Close incurs.
	_ = clientConn.CloseNow()

	err := <-done
	require.Error(t, err)
	require.Contains(t, err.Error(), "read websocket message")
}

func TestSessionRunExpectedDisconnect(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := NewRegistry(log.Noop())
	serverConn, clientConn := testWSPair(t)
	sess := NewFactory(
		log.Noop(),
		reg,
		mock_repository.NewMockJobRepository(ctrl),
		mock_repository.NewMockLockRepository(ctrl),
		mock_scm.NewMockPublisher(ctrl),
	).New("agent-1", serverConn)
	require.NoError(t, reg.Register(context.Background(), sess))

	done := make(chan error, 1)
	go func() { done <- sess.Run(context.Background()) }()

	_ = clientConn.Close(websocket.StatusGoingAway, "")
	require.NoError(t, <-done)
}

func TestSessionWrite(t *testing.T) {
	serverConn, clientConn := testWSPair(t)
	sess := &session{id: "agent-1", conn: serverConn, logger: log.Noop()}

	ctx := context.Background()
	require.NoError(t, sess.Write(ctx, &terraplanev1.TerraformEnvelope{
		JobId: "job-1",
		Payload: &terraplanev1.TerraformEnvelope_Plan{
			Plan: &terraplanev1.PlanCommand{Repo: "acme/infra"},
		},
	}))

	var got terraplanev1.TerraformEnvelope
	require.NoError(t, wsproto.Read(ctx, clientConn, &got))
	require.Equal(t, "job-1", got.GetJobId())
	require.NotNil(t, got.GetPlan())
}

func testWSPair(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()

	ready := make(chan *websocket.Conn, 1)
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		ready <- c
		<-hold
	}))
	t.Cleanup(func() {
		close(hold)
		srv.Close()
	})

	client, _, err := websocket.Dial(context.Background(), srv.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })

	select {
	case server = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket accept")
	}
	t.Cleanup(func() { _ = server.CloseNow() })
	return server, client
}
