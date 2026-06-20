package terraform

import (
	"context"
	"fmt"
	"strings"

	"github.com/xyzjace/terraplane/internal/process"
)

type Runner interface {
	Init(ctx context.Context, terraformBin, workDir string) error
	Plan(ctx context.Context, terraformBin, workDir, planFlags string) (string, error)
}

type runner struct{}

func NewRunner() Runner {
	return &runner{}
}

func (r *runner) Init(ctx context.Context, terraformBin, workDir string) error {
	// TODO: We need to be able to supply TF_VAR somehow
	result, err := r.run(ctx, terraformBin, workDir, "init", "-no-color", "-input=false")
	if err != nil {
		return fmt.Errorf("failed to run terraform init: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("terraform init failed with exit code %d: %s", result.ExitCode, commandOutput(result))
	}
	return nil
}

func (r *runner) Plan(ctx context.Context, terraformBin, workDir, planFlags string) (string, error) {
	args := []string{"plan", "-no-color", "-input=false"}
	if planFlags != "" {
		args = append(args, strings.Fields(planFlags)...)
	}

	result, err := r.run(ctx, terraformBin, workDir, args...)
	if err != nil {
		return "", fmt.Errorf("failed to run terraform plan: %w", err)
	}
	output := commandOutput(result)
	if result.ExitCode != 0 {
		return output, fmt.Errorf("terraform plan failed with exit code %d", result.ExitCode)
	}
	return output, nil
}

func (r *runner) run(ctx context.Context, terraformBin, workDir string, args ...string) (process.Result, error) {
	return process.Run(ctx, process.Command{
		Name: terraformBin,
		Args: args,
		Dir:  workDir,
	})
}

func commandOutput(result process.Result) string {
	if result.Stderr != "" {
		if result.Stdout != "" {
			return result.Stdout + "\n" + result.Stderr
		}
		return result.Stderr
	}
	return result.Stdout
}
