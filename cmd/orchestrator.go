package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var orchestratorCmd = &cobra.Command{
	Use:   "orchestrator",
	Short: "The terraplane orchestrator and API layer",
	RunE: func(cmd *cobra.Command, args []string) error {
		orchestrator, err := InitializeOrchestrator()
		if err != nil {
			fmt.Printf("Failed to initialize orchestrator: %v\n", err)
			return err
		}
		ctx := cmd.Context()
		err = orchestrator.Start(ctx)
		if err != nil {
			fmt.Printf("Orchestrator encountered an error: %v\n", err)
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(orchestratorCmd)
}
