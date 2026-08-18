package webserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/agentapi"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/repository"
	"github.com/xyzjace/terraplane/pkg/webserver"
)

type stubJobs struct {
	err       error
	called    chan *scm.Webhook
	pollJob   *agentapi.Job
	pollErr   error
	ackErr    error
	hbErr     error
	result    *agentapi.Result
	resultErr error
}

func (s *stubJobs) CreatePendingJobs(_ context.Context, webhook *scm.Webhook) error {
	if s.called != nil {
		s.called <- webhook
	}
	return s.err
}
func (s *stubJobs) ClaimPendingJobs(context.Context, []string) ([]command.Command, error) {
	return nil, nil
}
func (s *stubJobs) PollJob(context.Context, string) (*agentapi.Job, error) {
	return s.pollJob, s.pollErr
}
func (s *stubJobs) AckJob(context.Context, string, string) error { return s.ackErr }
func (s *stubJobs) Heartbeat(context.Context, string, string) error {
	return s.hbErr
}
func (s *stubJobs) RecordResult(_ context.Context, _, _ string, result agentapi.Result) error {
	s.result = &result
	return s.resultErr
}
func (s *stubJobs) FailClaimedJob(context.Context, string, string) error {
	return nil
}
func (s *stubJobs) ReapExpiredClaims(context.Context) error { return nil }

type HandlerSuite struct {
	suite.Suite
	ctrl      *gomock.Controller
	scm       *mock_scm.MockProvider
	publisher *mock_scm.MockPublisher
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
	s.jobs = &stubJobs{called: make(chan *scm.Webhook, 8)}
	s.handler = webserver.NewHandler(log.Noop(), s.scm, s.publisher, s.jobs, &config.Config{SharedAuthToken: "secret"})
}

func (s *HandlerSuite) agentReq(method, path string, body string) *http.Request {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(agentapi.AgentIDHeader, "agent-a")
	return req
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

func (s *HandlerSuite) TestAgentUnauthorized() {
	req := httptest.NewRequest(http.MethodPost, "/agent/jobs/poll", nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusUnauthorized, rec.Code)

	for _, path := range []string{
		"/agent/jobs/job-1/ack",
		"/agent/jobs/job-1/heartbeat",
		"/agent/jobs/job-1/result",
	} {
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(s.T(), http.StatusUnauthorized, rec.Code, path)
	}
}

func (s *HandlerSuite) TestAgentMissingID() {
	req := httptest.NewRequest(http.MethodPost, "/agent/jobs/poll", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusBadRequest, rec.Code)
}

func (s *HandlerSuite) TestAgentPollNoContent() {
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/poll", ""))
	require.Equal(s.T(), http.StatusNoContent, rec.Code)
}

func (s *HandlerSuite) TestAgentPollReturnsJob() {
	s.jobs.pollJob = &agentapi.Job{JobID: "job-1", Action: "plan", StackName: "a"}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/poll", ""))
	require.Equal(s.T(), http.StatusOK, rec.Code)
	var got agentapi.Job
	require.NoError(s.T(), json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(s.T(), "job-1", got.JobID)
}

func (s *HandlerSuite) TestAgentPollError() {
	s.jobs.pollErr = errors.New("db")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/poll", ""))
	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
}

func (s *HandlerSuite) TestAgentAckHeartbeatResult() {
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/ack", ""))
	require.Equal(s.T(), http.StatusNoContent, rec.Code)

	rec = httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/heartbeat", ""))
	require.Equal(s.T(), http.StatusNoContent, rec.Code)

	rec = httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/result", `{"success":true,"output":"ok"}`))
	require.Equal(s.T(), http.StatusNoContent, rec.Code)
	require.NotNil(s.T(), s.jobs.result)
	require.True(s.T(), s.jobs.result.Success)
}

func (s *HandlerSuite) TestAgentAckNotFound() {
	s.jobs.ackErr = repository.ErrJobNotFound
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/ack", ""))
	require.Equal(s.T(), http.StatusNotFound, rec.Code)
}

func (s *HandlerSuite) TestAgentHeartbeatError() {
	s.jobs.hbErr = errors.New("db")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/heartbeat", ""))
	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
}

func (s *HandlerSuite) TestAgentResultBadJSON() {
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/result", `{`))
	require.Equal(s.T(), http.StatusBadRequest, rec.Code)
}

func (s *HandlerSuite) TestAgentResultNotFound() {
	s.jobs.resultErr = repository.ErrJobNotFound
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/result", `{"success":true}`))
	require.Equal(s.T(), http.StatusNotFound, rec.Code)
}

func (s *HandlerSuite) TestAgentResultError() {
	s.jobs.resultErr = errors.New("db")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, s.agentReq(http.MethodPost, "/agent/jobs/job-1/result", `{"success":false}`))
	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
}

func TestServerStartShutdown(t *testing.T) {
	cfg := &config.Config{
		OrchestratorListenAddress: "127.0.0.1",
		OrchestratorListenPort:    0,
	}
	srv := webserver.NewServer(cfg, log.Noop(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan error, 1)
	go func() { started <- srv.Start(ctx) }()

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
		OrchestratorListenPort:    -1,
	}
	srv := webserver.NewServer(cfg, log.Noop(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	err := srv.Start(context.Background())
	require.Error(t, err)
}
