package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyzjace/terraplane/internal/process"
)

type Runner interface {
	Init(ctx context.Context, terraformBin, workDir string) error
	Plan(ctx context.Context, terraformBin, workDir, planFlags string) (string, error)
	Apply(ctx context.Context, terraformBin, workDir string) (string, error)
}

type runner struct {
	jobID string
}

func NewRunner(jobID string) Runner {
	return &runner{jobID: jobID}
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
	planFile := "plan.tfplan"
	if err := removeStalePlanFiles(workDir, planFile); err != nil {
		return "", fmt.Errorf("remove stale plan files in %q: %w", workDir, err)
	}

	args := []string{"plan", "-no-color", "-input=false", "-out=" + planFile}
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

func (r *runner) Apply(ctx context.Context, terraformBin, workDir string) (string, error) {
	// TODO: This uses the plan job id which changes for the apply job. What to do here? We really need to think about what jobs actually mean, and what locking really means
	planFile := "plan.tfplan"
	if err := removeStalePlanFiles(workDir, planFile); err != nil {
		return "", fmt.Errorf("remove stale plan files in %q: %w", workDir, err)
	}

	planPath := filepath.Join(workDir, planFile)
	if _, err := os.Stat(planPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("plan file %q not found in %q", planFile, workDir)
		}
		return "", fmt.Errorf("stat plan file %q: %w", planPath, err)
	}

	args := []string{"apply", "-no-color", "-input=false", planFile}

	result, err := r.run(ctx, terraformBin, workDir, args...)
	if err != nil {
		return "", fmt.Errorf("failed to run terraform apply: %w", err)
	}
	output := commandOutput(result)
	if result.ExitCode != 0 {
		return output, fmt.Errorf("terraform apply failed with exit code %d", result.ExitCode)
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

func removeStalePlanFiles(workDir, keepPlanFile string) error {
	matches, err := filepath.Glob(filepath.Join(workDir, "plan-*.tfplan"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if filepath.Base(path) == keepPlanFile {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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
