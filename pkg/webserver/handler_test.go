package webserver_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/agentsession/mock_agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/webserver"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

type stubJobs struct {
	err      error
	called   chan *scm.Webhook
	claimCmd *command.Command
	claimErr error
}

func (s *stubJobs) CreatePendingJobs(_ context.Context, webhook *scm.Webhook) error {
	if s.called != nil {
		s.called <- webhook
	}
	return s.err
}

func (s *stubJobs) ClaimPendingJob(context.Context, string) (*command.Command, error) {
	return s.claimCmd, s.claimErr
}
func (s *stubJobs) ReleaseClaim(context.Context, string) error { return nil }
func (s *stubJobs) FailClaimedJob(context.Context, string, string) error {
	return nil
}
func (s *stubJobs) ReapExpiredClaims(context.Context) error { return nil }

type stubFactory struct {
	session agentsession.Session
}

func (f stubFactory) New(string, *websocket.Conn) agentsession.Session { return f.session }

type stubSession struct {
	id     string
	runErr error
	runCh  chan struct{}
}

func (s *stubSession) ID() string { return s.id }
func (s *stubSession) Run(context.Context) error {
	if s.runCh != nil {
		close(s.runCh)
	}
	return s.runErr
}
func (s *stubSession) Write(context.Context, *terraplanev1.TerraformEnvelope) error { return nil }

type HandlerSuite struct {
	suite.Suite
	ctrl      *gomock.Controller
	scm       *mock_scm.MockProvider
	publisher *mock_scm.MockPublisher
	registry  agentsession.Registry
	jobs      *stubJobs
	handler   http.Handler
}

func TestHandlerSuite(t *testing.T) {
	suite.Run(t, new(HandlerSuite))
}

func (s *HandlerSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.publisher = mock_scm.NewMockPublisher(s.ctrl)
	s.registry = agentsession.NewRegistry(log.Noop())
	s.jobs = &stubJobs{called: make(chan *scm.Webhook, 8)}
	s.handler = s.newHandler(s.registry, stubFactory{session: &stubSession{id: "agent-1"}})
}

func (s *HandlerSuite) newHandler(registry agentsession.Registry, factory agentsession.Factory) http.Handler {
	return webserver.NewHandler(
		log.Noop(),
		s.scm,
		s.publisher,
		registry,
		factory,
		s.jobs,
		&config.Config{SharedAuthToken: "secret"},
	)
}

func (s *HandlerSuite) TestHealthCheck() {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusOK, rec.Code)
	require.Equal(s.T(), "OK", rec.Body.String())
}

func (s *HandlerSuite) TestWebhookParseFailure() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return(nil, errors.New("bad signature"))

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)

	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
	require.Contains(s.T(), rec.Body.String(), "Failed to parse SCM webhook")
}

func (s *HandlerSuite) TestWebhookNoActionableEvents() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)

	require.Equal(s.T(), http.StatusOK, rec.Code)
	require.Contains(s.T(), rec.Body.String(), "No actionable event found")
}

func (s *HandlerSuite) TestWebhookIgnoresUnknownCommands() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{{
		RepositorySlug: "acme/infra",
		PRNumber:       1,
		FullCommand:    "not a terraplane command",
		TriggeringUser: "jace",
		CommitSHA:      "abc",
	}}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 0).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)

	require.Equal(s.T(), http.StatusOK, rec.Code)
	require.Contains(s.T(), rec.Body.String(), "Webhook parsed successfully")

	select {
	case got := <-s.jobs.called:
		require.Equal(s.T(), "not a terraplane command", got.FullCommand)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for CreatePendingJobs")
	}
}

func (s *HandlerSuite) TestWebhookEnqueuesPendingJobs() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane plan -s a", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 11},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane apply -s a", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 12},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane unlock -s a", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 13},
	}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 11).Return(nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 12).Return(nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 13).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusOK, rec.Code)

	for _, want := range []string{"terraplane plan -s a", "terraplane apply -s a", "terraplane unlock -s a"} {
		select {
		case got := <-s.jobs.called:
			require.Equal(s.T(), want, got.FullCommand)
		case <-time.After(2 * time.Second):
			s.T().Fatalf("timed out waiting for CreatePendingJobs (%s)", want)
		}
	}
}

func (s *HandlerSuite) TestWebhookServiceErrorsAreLoggedNotReturned() {
	// Intention: webhook ACK stays 200 even when enqueue fails.
	s.jobs.err = errors.New("upsert failed")

	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane plan", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 21},
	}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 21).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusOK, rec.Code)

	select {
	case <-s.jobs.called:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for CreatePendingJobs")
	}
}

func (s *HandlerSuite) TestWebhookAcknowledgeFailureStillEnqueues() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{{
		RepositorySlug: "acme/infra",
		PRNumber:       1,
		FullCommand:    "terraplane plan -s a",
		TriggeringUser: "jace",
		CommitSHA:      "abc",
		CommentID:      99,
	}}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 99).Return(errors.New("react failed"))

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusOK, rec.Code)

	select {
	case got := <-s.jobs.called:
		require.Equal(s.T(), "terraplane plan -s a", got.FullCommand)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for CreatePendingJobs")
	}
}

func (s *HandlerSuite) TestBearerRequiredOnAgentRoutes() {
	paths := []string{
		"/ws",
		"/agent/jobs/claim",
		"/agent/jobs/job-1/heartbeat",
		"/agent/jobs/job-1/ack",
		"/agent/jobs/job-1/result",
	}
	for _, path := range paths {
		method := http.MethodPost
		if path == "/ws" {
			method = http.MethodGet
		}
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		require.Equal(s.T(), http.StatusUnauthorized, rec.Code, path)

		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer wrong")
		rec = httptest.NewRecorder()
		s.handler.ServeHTTP(rec, req)
		require.Equal(s.T(), http.StatusUnauthorized, rec.Code, path)
	}
}

func (s *HandlerSuite) TestAgentRoutesAcceptBearerToken() {
	for _, path := range []string{
		"/agent/jobs/job-1/heartbeat",
		"/agent/jobs/job-1/ack",
		"/agent/jobs/job-1/result",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, req)
		require.Equal(s.T(), http.StatusOK, rec.Code, path)
	}
}

func (s *HandlerSuite) TestAgentClaimNoJob() {
	req := httptest.NewRequest(http.MethodPost, "/agent/jobs/claim", strings.NewReader(`{"agent_id":"agent-dev"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusNoContent, rec.Code)
}

func (s *HandlerSuite) TestAgentClaimReturnsJob() {
	cmd := command.Command{Kind: command.KindPlan}
	cmd.Plan.JobID = "job-1"
	s.jobs.claimCmd = &cmd

	req := httptest.NewRequest(http.MethodPost, "/agent/jobs/claim", strings.NewReader(`{"agent_id":"agent-dev"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusOK, rec.Code)
	require.Contains(s.T(), rec.Body.String(), `"Kind":"plan"`)
	require.Contains(s.T(), rec.Body.String(), "job-1")
}

func (s *HandlerSuite) TestAgentClaimInvalidJSON() {
	req := httptest.NewRequest(http.MethodPost, "/agent/jobs/claim", strings.NewReader(`{`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
}

func (s *HandlerSuite) TestAgentClaimServiceError() {
	s.jobs.claimErr = errors.New("db")
	req := httptest.NewRequest(http.MethodPost, "/agent/jobs/claim", strings.NewReader(`{"agent_id":"agent-dev"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
}

func (s *HandlerSuite) TestWebsocketAcceptFailure() {
	// httptest.ResponseRecorder cannot hijack, so websocket.Accept fails.
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
}

func (s *HandlerSuite) TestWebsocketHappyPath() {
	runCh := make(chan struct{})
	sess := &stubSession{id: "agent-42", runCh: runCh}
	s.handler = s.newHandler(s.registry, stubFactory{session: sess})

	srv := httptest.NewServer(s.handler)
	s.T().Cleanup(srv.Close)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
	require.NoError(s.T(), err)
	s.T().Cleanup(func() { _ = conn.CloseNow() })

	require.NoError(s.T(), wsproto.Write(ctx, conn, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Hello{
			Hello: &terraplanev1.Hello{AgentId: "agent-42"},
		},
	}))

	select {
	case <-runCh:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for session.Run")
	}

	got, err := s.registry.Get(ctx, "agent-42")
	require.NoError(s.T(), err)
	require.NotNil(s.T(), got)
	require.Equal(s.T(), "agent-42", got.ID())
}

func (s *HandlerSuite) TestWebsocketMissingHelloPayload() {
	srv := httptest.NewServer(s.handler)
	s.T().Cleanup(srv.Close)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
	require.NoError(s.T(), err)
	s.T().Cleanup(func() { _ = conn.CloseNow() })

	// Goodbye is not a hello.
	require.NoError(s.T(), wsproto.Write(ctx, conn, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Goodbye{
			Goodbye: &terraplanev1.Goodbye{AgentId: "x"},
		},
	}))

	_, _, readErr := conn.Read(ctx)
	require.Error(s.T(), readErr)
}

func (s *HandlerSuite) TestWebsocketEmptyAgentID() {
	srv := httptest.NewServer(s.handler)
	s.T().Cleanup(srv.Close)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
	require.NoError(s.T(), err)
	s.T().Cleanup(func() { _ = conn.CloseNow() })

	require.NoError(s.T(), wsproto.Write(ctx, conn, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Hello{
			Hello: &terraplanev1.Hello{AgentId: ""},
		},
	}))

	_, _, readErr := conn.Read(ctx)
	require.Error(s.T(), readErr)
}

func (s *HandlerSuite) TestWebsocketHelloReadFailure() {
	srv := httptest.NewServer(s.handler)
	s.T().Cleanup(srv.Close)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
	require.NoError(s.T(), err)
	_ = conn.CloseNow()
}

func (s *HandlerSuite) TestWebsocketRegisterFailure() {
	reg := mock_agentsession.NewMockRegistry(s.ctrl)
	reg.EXPECT().Register(gomock.Any(), gomock.Any()).Return(errors.New("registry full"))

	s.handler = s.newHandler(reg, stubFactory{session: &stubSession{id: "agent-1"}})

	srv := httptest.NewServer(s.handler)
	s.T().Cleanup(srv.Close)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
	require.NoError(s.T(), err)
	s.T().Cleanup(func() { _ = conn.CloseNow() })

	require.NoError(s.T(), wsproto.Write(ctx, conn, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Hello{
			Hello: &terraplanev1.Hello{AgentId: "agent-1"},
		},
	}))

	_, _, readErr := conn.Read(ctx)
	require.Error(s.T(), readErr)
}

func (s *HandlerSuite) TestWebsocketSessionRunErrorIsLogged() {
	runCh := make(chan struct{})
	sess := &stubSession{id: "agent-err", runErr: errors.New("session boom"), runCh: runCh}
	s.handler = s.newHandler(s.registry, stubFactory{session: sess})

	srv := httptest.NewServer(s.handler)
	s.T().Cleanup(srv.Close)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
	require.NoError(s.T(), err)
	s.T().Cleanup(func() { _ = conn.CloseNow() })

	require.NoError(s.T(), wsproto.Write(ctx, conn, &terraplanev1.WebsocketEnvelope{
		Payload: &terraplanev1.WebsocketEnvelope_Hello{
			Hello: &terraplanev1.Hello{AgentId: "agent-err"},
		},
	}))

	select {
	case <-runCh:
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for session.Run")
	}
}

func TestServerStartShutdown(t *testing.T) {
	cfg := &config.Config{
		OrchestratorListenAddress: "127.0.0.1",
		OrchestratorListenPort:    0, // ephemeral
	}
	srv := webserver.NewServer(cfg, log.Noop(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan error, 1)
	go func() { started <- srv.Start(ctx) }()

	// Give ListenAndServe a moment to bind.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return on cancel")
	}

	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestServerStartListenError(t *testing.T) {
	cfg := &config.Config{
		OrchestratorListenAddress: "127.0.0.1",
		OrchestratorListenPort:    -1, // invalid
	}
	srv := webserver.NewServer(cfg, log.Noop(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	err := srv.Start(context.Background())
	require.Error(t, err)
}
