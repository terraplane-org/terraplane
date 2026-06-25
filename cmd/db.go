package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xyzjace/terraplane/config"
	"github.com/xyzjace/terraplane/pkg/storage"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply database schema migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewConfig()
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		if err := storage.Migrate(cmd.Context(), cfg); err != nil {
			return err
		}

		fmt.Println("database migrations applied successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.AddCommand(dbMigrateCmd)
}
