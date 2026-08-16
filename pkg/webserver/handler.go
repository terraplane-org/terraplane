package webserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/wsproto"
)

const agentHelloTimeout = 10 * time.Second

type handler struct {
	logger          log.Logger
	scmProvider     scm.Provider
	scmPublisher    scm.Publisher
	mux             *http.ServeMux
	sessionRegistry agentsession.Registry
	sessionFactory  agentsession.Factory
	planService     services.PlanService
	applyService    services.ApplyService
	unlockService   services.UnlockService
	sharedAuthToken string
	jobService      services.JobService
}

func NewHandler(
	logger log.Logger,
	scmProvider scm.Provider,
	scmPublisher scm.Publisher,
	sessionRegistry agentsession.Registry,
	sessionFactory agentsession.Factory,
	planService services.PlanService,
	applyService services.ApplyService,
	unlockService services.UnlockService,
	jobService services.JobService,
	config *config.Config,
) http.Handler {
	h := &handler{
		logger:          logger,
		mux:             http.NewServeMux(),
		scmProvider:     scmProvider,
		scmPublisher:    scmPublisher,
		sessionRegistry: sessionRegistry,
		sessionFactory:  sessionFactory,
		planService:     planService,
		applyService:    applyService,
		unlockService:   unlockService,
		sharedAuthToken: config.SharedAuthToken,
		jobService:      jobService,
	}

	h.mux.HandleFunc("GET /health", h.healthCheck)
	h.mux.HandleFunc("POST /scm/webhook", h.scmWebhookHandler)
	h.mux.HandleFunc("GET /ws", h.websocketHandler)

	return h
}

func (h *handler) scmWebhookHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("SCM webhook handler called")

	webhooks, err := h.scmProvider.ParseWebhook(r)
	if err != nil {
		h.logger.Error("Failed to parse SCM webhook", "error", err)
		writeResponse(w, http.StatusInternalServerError, "Failed to parse SCM webhook")
		return
	}
	if len(webhooks) == 0 {
		h.logger.Debug("No actionable SCM webhook events found")
		writeResponse(w, http.StatusOK, "No actionable event found")
		return
	}

	for _, webhook := range webhooks {

		_ = h.jobService.CreatePendingJobs(r.Context(), &webhook)

		// TODO: Is this really the appropriate place to react to the comment?
		if err := h.scmPublisher.AcknowledgeComment(
			r.Context(),
			webhook.RepositorySlug,
			webhook.PRNumber,
			webhook.CommentID,
		); err != nil {
			h.logger.Error(
				"Failed to acknowledge pull request comment",
				"repo", webhook.RepositorySlug,
				"pr", webhook.PRNumber,
				"comment_id", webhook.CommentID,
				"error", err,
			)
		}
		// h.handleCommand(r.Context(), cmd)
	}

	writeResponse(w, http.StatusOK, "Webhook parsed successfully")
}

func (h *handler) handleCommand(ctx context.Context, cmd command.Command) {
	switch cmd.Kind {
	case command.KindPlan:
		h.logger.Info(
			"Received terraplane plan command",
			"repo", cmd.Plan.Repo,
			"pr", cmd.Plan.PRNumber,
			"user", cmd.Plan.TriggerUser,
			"commit", cmd.Plan.CommitSHA,
			"stacks", cmd.Plan.Stacks,
			"environments", cmd.Plan.Environments,
			"plan_flags", cmd.Plan.PlanFlags,
			"comment", cmd.Plan.RawComment,
		)
		plan := cmd.Plan
		go func() {
			if err := h.planService.RunPlan(context.WithoutCancel(ctx), plan); err != nil {
				h.logger.Error(
					"Failed to run terraplane plan",
					"repo", plan.Repo,
					"pr", plan.PRNumber,
					"error", err,
				)
			}
		}()
	case command.KindApply:
		h.logger.Info(
			"Received terraplane apply command",
			"repo", cmd.Apply.Repo,
			"pr", cmd.Apply.PRNumber,
			"user", cmd.Apply.TriggerUser,
			"commit", cmd.Apply.CommitSHA,
			"stacks", cmd.Apply.Stacks,
			"environments", cmd.Apply.Environments,
			"comment", cmd.Apply.RawComment,
		)
		apply := cmd.Apply
		go func() {
			if err := h.applyService.RunApply(context.WithoutCancel(ctx), apply); err != nil {
				h.logger.Error(
					"Failed to run terraplane apply",
					"repo", apply.Repo,
					"pr", apply.PRNumber,
					"error", err,
				)
			}
		}()
	case command.KindUnlock:
		h.logger.Info(
			"Received terraplane unlock command",
			"repo", cmd.Unlock.Repo,
			"pr", cmd.Unlock.PRNumber,
			"user", cmd.Unlock.TriggerUser,
			"commit", cmd.Unlock.CommitSHA,
			"stacks", cmd.Unlock.Stacks,
			"environments", cmd.Unlock.Environments,
			"comment", cmd.Unlock.RawComment,
		)
		unlock := cmd.Unlock
		go func() {
			if err := h.unlockService.RunUnlock(context.WithoutCancel(ctx), unlock); err != nil {
				h.logger.Error(
					"Failed to run terraplane unlock",
					"repo", unlock.Repo,
					"pr", unlock.PRNumber,
					"error", err,
				)
			}
		}()
	default:
		h.logger.Warn("Received terraplane command with unhandled kind", "kind", cmd.Kind)
	}
}

func (h *handler) websocketHandler(w http.ResponseWriter, r *http.Request) {
	if !h.validateWebsocketToken(r) {
		h.logger.Warn("Rejected websocket connection with invalid auth token", "remote_addr", r.RemoteAddr)
		writeResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	conn, err := websocket.Accept(w, r, wsproto.AcceptOptions())
	if err != nil {
		h.logger.Error("Failed to accept websocket connection", "error", err)
		return
	}
	wsproto.ConfigureConn(conn)

	helloCtx, cancel := context.WithTimeout(r.Context(), agentHelloTimeout)
	defer cancel()

	var hello terraplanev1.WebsocketEnvelope
	if err := wsproto.Read(helloCtx, conn, &hello); err != nil {
		h.logger.Error("Failed to read agent hello", "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "failed to read agent hello")
		return
	}

	agentID, err := agentIDFromHello(&hello)
	if err != nil {
		h.logger.Error("Invalid agent hello", "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	h.logger.Info("Received agent hello", "agent_id", agentID)

	session := h.sessionFactory.New(agentID, conn)

	if err := h.sessionRegistry.Register(r.Context(), session); err != nil {
		h.logger.Error("Failed to register agent session", "agent_id", agentID, "error", err)
		_ = conn.Close(websocket.StatusInternalError, "failed to register agent session")
		return
	}

	// TODO: Stop agent from being killed by various "errors" from the websocket connection like ACK receive
	if err := session.Run(r.Context()); err != nil {
		h.logger.Error("Agent session ended with error", "agent_id", agentID, "error", err)
	}
}

func (h *handler) validateWebsocketToken(r *http.Request) bool {
	return auth.BearerTokenMatches(r.Header.Get("Authorization"), h.sharedAuthToken)
}

func agentIDFromHello(hello *terraplanev1.WebsocketEnvelope) (string, error) {
	helloMsg := hello.GetHello()
	if helloMsg == nil {
		return "", fmt.Errorf("expected hello payload")
	}
	if helloMsg.GetAgentId() == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	return helloMsg.GetAgentId(), nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, http.StatusOK, "OK")
}

func writeResponse(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
