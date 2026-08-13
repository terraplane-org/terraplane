package webserver

import (
	"context"
	"net/http"

	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/internal/auth"
	"github.com/xyzjace/terraplane/pkg/command"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/orchestrator/services"
	"github.com/xyzjace/terraplane/pkg/scm"
)

type handler struct {
	logger          log.Logger
	scmProvider     scm.Provider
	scmPublisher    scm.Publisher
	mux             *http.ServeMux
	planService     services.PlanService
	applyService    services.ApplyService
	unlockService   services.UnlockService
	claimService    services.JobClaimService
	resultService   services.JobResultService
	sharedAuthToken string
}

func NewHandler(
	logger log.Logger,
	scmProvider scm.Provider,
	scmPublisher scm.Publisher,
	planService services.PlanService,
	applyService services.ApplyService,
	unlockService services.UnlockService,
	claimService services.JobClaimService,
	resultService services.JobResultService,
	cfg *config.Config,
) http.Handler {
	h := &handler{
		logger:          logger,
		mux:             http.NewServeMux(),
		scmProvider:     scmProvider,
		scmPublisher:    scmPublisher,
		planService:     planService,
		applyService:    applyService,
		unlockService:   unlockService,
		claimService:    claimService,
		resultService:   resultService,
		sharedAuthToken: cfg.SharedAuthToken,
	}

	h.mux.HandleFunc("GET /health", h.healthCheck)
	h.mux.HandleFunc("POST /scm/webhook", h.scmWebhookHandler)
	h.mux.HandleFunc("POST /api/v1/agents/{agentID}/jobs/claim", h.agentClaimHandler)
	h.mux.HandleFunc("POST /api/v1/agents/{agentID}/jobs/{jobID}/result", h.agentResultHandler)

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
		cmd := command.ParseWebhook(&webhook)
		if cmd.Kind == command.KindUnknown {
			h.logger.Debug(
				"Ignoring pull request comment that is not a terraplane command",
				"repo", webhook.RepositorySlug,
				"pr", webhook.PRNumber,
				"user", webhook.TriggeringUser,
				"comment", webhook.FullCommand,
			)
			continue
		}
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
		h.handleCommand(r.Context(), cmd)
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

func authBearerMatches(r *http.Request, token string) bool {
	return auth.BearerTokenMatches(r.Header.Get("Authorization"), token)
}
