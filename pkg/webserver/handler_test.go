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

type stubPlan struct {
	err    error
	called chan command.PlanCommand
}

func (s *stubPlan) RunPlan(_ context.Context, plan command.PlanCommand) error {
	if s.called != nil {
		s.called <- plan
	}
	return s.err
}

type stubApply struct {
	err    error
	called chan command.ApplyCommand
}

func (s *stubApply) RunApply(_ context.Context, apply command.ApplyCommand) error {
	if s.called != nil {
		s.called <- apply
	}
	return s.err
}

type stubUnlock struct {
	err    error
	called chan command.UnlockCommand
}

func (s *stubUnlock) RunUnlock(_ context.Context, unlock command.UnlockCommand) error {
	if s.called != nil {
		s.called <- unlock
	}
	return s.err
}

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
	plan      *stubPlan
	apply     *stubApply
	unlock    *stubUnlock
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
	s.plan = &stubPlan{called: make(chan command.PlanCommand, 1)}
	s.apply = &stubApply{called: make(chan command.ApplyCommand, 1)}
	s.unlock = &stubUnlock{called: make(chan command.UnlockCommand, 1)}
	s.handler = webserver.NewHandler(
		log.Noop(),
		s.scm,
		s.publisher,
		s.registry,
		stubFactory{session: &stubSession{id: "agent-1"}},
		s.plan,
		s.apply,
		s.unlock,
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

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)

	require.Equal(s.T(), http.StatusOK, rec.Code)
	require.Contains(s.T(), rec.Body.String(), "Webhook parsed successfully")
	select {
	case <-s.plan.called:
		s.T().Fatal("plan should not run for unknown commands")
	case <-time.After(50 * time.Millisecond):
	}
}

func (s *HandlerSuite) TestWebhookDispatchesPlanApplyUnlock() {
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

	select {
	case plan := <-s.plan.called:
		require.Equal(s.T(), []string{"a"}, plan.Stacks)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for plan")
	}
	select {
	case apply := <-s.apply.called:
		require.Equal(s.T(), []string{"a"}, apply.Stacks)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for apply")
	}
	select {
	case unlock := <-s.unlock.called:
		require.Equal(s.T(), []string{"a"}, unlock.Stacks)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for unlock")
	}
}

func (s *HandlerSuite) TestWebhookServiceErrorsAreLoggedNotReturned() {
	// Intention: webhook ACK stays 200 even when async service work fails.
	s.plan.err = errors.New("plan failed")
	s.apply.err = errors.New("apply failed")
	s.unlock.err = errors.New("unlock failed")

	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane plan", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 21},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane apply", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 22},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane unlock", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 23},
	}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 21).Return(nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 22).Return(nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 23).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/scm/webhook", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusOK, rec.Code)

	<-s.plan.called
	<-s.apply.called
	<-s.unlock.called
}

func (s *HandlerSuite) TestWebhookAcknowledgeFailureStillDispatches() {
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
	case plan := <-s.plan.called:
		require.Equal(s.T(), []string{"a"}, plan.Stacks)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timed out waiting for plan")
	}
}

func (s *HandlerSuite) TestWebsocketUnauthorized() {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusUnauthorized, rec.Code)
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
	s.handler = webserver.NewHandler(
		log.Noop(), s.scm, s.publisher, s.registry, stubFactory{session: sess},
		s.plan, s.apply, s.unlock,
		&config.Config{SharedAuthToken: "secret"},
	)

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

	s.handler = webserver.NewHandler(
		log.Noop(), s.scm, s.publisher, reg, stubFactory{session: &stubSession{id: "agent-1"}},
		s.plan, s.apply, s.unlock,
		&config.Config{SharedAuthToken: "secret"},
	)

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
	s.handler = webserver.NewHandler(
		log.Noop(), s.scm, s.publisher, s.registry, stubFactory{session: sess},
		s.plan, s.apply, s.unlock,
		&config.Config{SharedAuthToken: "secret"},
	)

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
