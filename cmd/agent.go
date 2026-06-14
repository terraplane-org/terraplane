package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "The terraplane agent is responsible for executing Terraform commands and reporting results back to the orchestrator.",
	RunE: func(cmd *cobra.Command, args []string) error {
		agent, err := InitializeAgent()
		if err != nil {
			fmt.Printf("Failed to initialize agent: %v\n", err)
			return err
		}
		ctx := cmd.Context()
		err = agent.Start(ctx)
		if err != nil {
			fmt.Printf("Agent encountered an error: %v\n", err)
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
}
