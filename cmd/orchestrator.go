package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var orchestratorCmd = &cobra.Command{
	Use:   "orchestrator",
	Short: "The terraplane orchestrator and API layer",
	Run: func(cmd *cobra.Command, args []string) {
		_, err := InitializeOrchestrator()
		if err != nil {
			fmt.Printf("Failed to initialize orchestrator: %v\n", err)
			return
		}
		fmt.Println("orchestrator called")
	},
}

func init() {
	rootCmd.AddCommand(orchestratorCmd)
}
