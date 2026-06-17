package services

import (
	"context"
	"fmt"

	"github.com/xyzjace/terraplane/pkg/agentsession"
	"github.com/xyzjace/terraplane/pkg/log"
	"github.com/xyzjace/terraplane/pkg/scm"
	"github.com/xyzjace/terraplane/pkg/scm/commands"
	terraplanev1 "github.com/xyzjace/terraplane/pkg/terraplane/v1"
	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
)

type PlanService interface {
	RunPlan(repo string, prNumber int, commitHash string, rawPlan string) error
}

type planService struct {
	logger        log.Logger
	agentRegistry agentsession.Registry
	scmProvider   scm.Provider
}

func (s *planService) RunPlan(repo string, prNumber int, commitHash string, rawPlan string) error {
	ctx := context.Background()

	file, err := s.scmProvider.GetFile(ctx, repo, "terraplane.yaml")
	if err != nil {
		return fmt.Errorf("fetch repository config: %w", err)
	}
	config, err := terraplaneconfig.ParseConfigFile([]byte(file))
	if err != nil {
		return fmt.Errorf("parse terraplane config: %w", err)
	}
	stacks, err := config.ResolveStacks(commands.ParsePlanStacks(rawPlan))
	if err != nil {
		return fmt.Errorf("resolve stacks: %w", err)
	}

	for _, stack := range stacks {
		agent, _ := s.agentRegistry.Get(ctx, stack.Agent)
		if agent == nil {
			continue
		}
		if err := agent.Write(ctx, &terraplanev1.TerraformEnvelope{
			Payload: &terraplanev1.TerraformEnvelope_Plan{
				Plan: &terraplanev1.PlanCommand{
					Repo:       repo,
					PrNumber:   int32(prNumber),
					CommitHash: commitHash,
					PlanFlags:  rawPlan,
				},
			},
		}); err != nil {
			return fmt.Errorf("plan stack %q: %w", stack.Name, err)
		}
	}
	return nil
}

func NewPlanService(logger log.Logger, agentRegistry agentsession.Registry, scmProvider scm.Provider) *planService {
	return &planService{
		logger:        logger,
		agentRegistry: agentRegistry,
		scmProvider:   scmProvider,
	}
}
