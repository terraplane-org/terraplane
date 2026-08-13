package webserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/scm/mock_scm"
	"github.com/xyzjace/terraplane/pkg/storage/models"
	"github.com/xyzjace/terraplane/pkg/webserver"
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

type stubClaim struct {
	job *models.Job
	err error
}

func (s *stubClaim) Claim(context.Context, string, time.Duration) (*models.Job, error) {
	return s.job, s.err
}

type stubResult struct {
	err error
}

func (s *stubResult) Complete(context.Context, string, string, bool, string, string) error {
	return s.err
}

func (s *stubResult) FailExpired(context.Context) error { return nil }

type HandlerSuite struct {
	suite.Suite
	ctrl      *gomock.Controller
	scm       *mock_scm.MockProvider
	publisher *mock_scm.MockPublisher
	plan      *stubPlan
	apply     *stubApply
	unlock    *stubUnlock
	claim     *stubClaim
	result    *stubResult
	handler   http.Handler
}

func TestHandlerSuite(t *testing.T) {
	suite.Run(t, new(HandlerSuite))
}

func (s *HandlerSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.scm = mock_scm.NewMockProvider(s.ctrl)
	s.publisher = mock_scm.NewMockPublisher(s.ctrl)
	s.plan = &stubPlan{called: make(chan command.PlanCommand, 1)}
	s.apply = &stubApply{called: make(chan command.ApplyCommand, 1)}
	s.unlock = &stubUnlock{called: make(chan command.UnlockCommand, 1)}
	s.claim = &stubClaim{}
	s.result = &stubResult{}
	s.handler = webserver.NewHandler(
		log.Noop(),
		s.scm,
		s.publisher,
		s.plan,
		s.apply,
		s.unlock,
		s.claim,
		s.result,
		&config.Config{SharedAuthToken: "secret"},
	)
}

func (s *HandlerSuite) TestHealth() {
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(s.T(), http.StatusOK, rec.Code)
	require.Equal(s.T(), "OK", rec.Body.String())
}

func (s *HandlerSuite) TestWebhookParseFailure() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return(nil, errors.New("bad"))
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/scm/webhook", nil))
	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
}

func (s *HandlerSuite) TestWebhookNoEvents() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return(nil, nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/scm/webhook", nil))
	require.Equal(s.T(), http.StatusOK, rec.Code)
}

func (s *HandlerSuite) TestWebhookDispatchesCommands() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane plan -s a", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 11},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane apply -s a", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 12},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane unlock -s a", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 13},
	}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 11).Return(nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 12).Return(nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 13).Return(nil)

	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/scm/webhook", nil))
	require.Equal(s.T(), http.StatusOK, rec.Code)

	select {
	case plan := <-s.plan.called:
		require.Equal(s.T(), []string{"a"}, plan.Stacks)
	case <-time.After(time.Second):
		s.T().Fatal("plan not called")
	}
	select {
	case apply := <-s.apply.called:
		require.Equal(s.T(), []string{"a"}, apply.Stacks)
	case <-time.After(time.Second):
		s.T().Fatal("apply not called")
	}
	select {
	case unlock := <-s.unlock.called:
		require.Equal(s.T(), []string{"a"}, unlock.Stacks)
	case <-time.After(time.Second):
		s.T().Fatal("unlock not called")
	}
}

func (s *HandlerSuite) TestWebhookIgnoresUnknown() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{
		{FullCommand: "hello"},
	}, nil)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/scm/webhook", nil))
	require.Equal(s.T(), http.StatusOK, rec.Code)
}

func (s *HandlerSuite) TestClaimUnauthorized() {
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-dev/jobs/claim", nil))
	require.Equal(s.T(), http.StatusUnauthorized, rec.Code)
}

func (s *HandlerSuite) TestClaimNoContent() {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-dev/jobs/claim?wait=0s", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusNoContent, rec.Code)
}

func (s *HandlerSuite) TestClaimReturnsJob() {
	s.claim.job = &models.Job{
		ID:        "job-1",
		Action:    models.JobActionPlan,
		Repo:      "acme/infra",
		PRNumber:  1,
		StackName: "a",
		Dir:       "stacks/a",
		CommitSHA: "abc",
		PlanFlags: "-target=x",
		AgentID:   "agent-dev",
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-dev/jobs/claim?wait=0s", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(s.T(), json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(s.T(), "job-1", body["id"])
	require.Equal(s.T(), "plan", body["action"])
}

func (s *HandlerSuite) TestClaimInvalidWait() {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-dev/jobs/claim?wait=nope", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusBadRequest, rec.Code)
}

func (s *HandlerSuite) TestClaimError() {
	s.claim.err = errors.New("db")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-dev/jobs/claim?wait=0s", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusInternalServerError, rec.Code)
}

func (s *HandlerSuite) TestResultSuccess() {
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/agent-dev/jobs/job-1/result",
		strings.NewReader(`{"success":true,"output":"ok"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusNoContent, rec.Code)
}

func (s *HandlerSuite) TestResultUnauthorized() {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-dev/jobs/job-1/result", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusUnauthorized, rec.Code)
}

func (s *HandlerSuite) TestResultBadJSON() {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-dev/jobs/job-1/result", strings.NewReader(`{`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusBadRequest, rec.Code)
}

func (s *HandlerSuite) TestResultCompleteError() {
	s.result.err = errors.New("nope")
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/agent-dev/jobs/job-1/result",
		strings.NewReader(`{"success":false}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	require.Equal(s.T(), http.StatusBadRequest, rec.Code)
}

func (s *HandlerSuite) TestWebhookAcknowledgeFailureStillDispatches() {
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane plan -s a", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 11},
	}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), "acme/infra", 1, 11).Return(errors.New("react failed"))

	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/scm/webhook", nil))
	require.Equal(s.T(), http.StatusOK, rec.Code)

	select {
	case <-s.plan.called:
	case <-time.After(time.Second):
		s.T().Fatal("plan not called")
	}
}

func (s *HandlerSuite) TestWebhookServiceErrorsAreLogged() {
	s.plan.err = errors.New("plan failed")
	s.apply.err = errors.New("apply failed")
	s.unlock.err = errors.New("unlock failed")
	s.scm.EXPECT().ParseWebhook(gomock.Any()).Return([]scm.Webhook{
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane plan", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 1},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane apply", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 2},
		{RepositorySlug: "acme/infra", PRNumber: 1, FullCommand: "terraplane unlock", TriggeringUser: "jace", CommitSHA: "abc", CommentID: 3},
	}, nil)
	s.publisher.EXPECT().AcknowledgeComment(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)

	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/scm/webhook", nil))
	require.Equal(s.T(), http.StatusOK, rec.Code)
	time.Sleep(50 * time.Millisecond)
}
