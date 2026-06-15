package services

import (
	"context"
	"fmt"

	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/log"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
)

type PlanService interface {
	RunPlan(repo string, prNumber int, commitHash string, planFlags string) error
}

type planService struct {
	logger        log.Logger
	agentRegistry agentsession.Registry
}

func (s *planService) RunPlan(repo string, prNumber int, commitHash string, planFlags string) error {
	ctx := context.Background()
	// TODO: Get the agent ID from the repository config file
	agent, err := s.agentRegistry.Get(ctx, "agent-dev")

	// TODO: Check if there's a plan already running for this specific combo
	// TODO: Do some DB operations to register that the plan has been started

	msg := &terraplanev1.TerraformEnvelope{
		Payload: &terraplanev1.TerraformEnvelope_Plan{
			Plan: &terraplanev1.PlanCommand{
				Repo:       repo,
				PrNumber:   int32(prNumber),
				CommitHash: commitHash,
				PlanFlags:  planFlags,
			},
		},
	}

	if err := agent.Write(ctx, msg); err != nil {
		return fmt.Errorf("write plan command: %w", err)
	}

	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	return nil
}

func NewPlanService(logger log.Logger, agentRegistry agentsession.Registry) *planService {
	return &planService{
		logger:        logger,
		agentRegistry: agentRegistry,
	}
}
