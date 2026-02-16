package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/mistic0xb/pekka/internal/db"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the database",
	Long:  `Creates the database file and schema if they don't exist.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := GetConfig()

		fmt.Printf("Initializing database at: %s\n", cfg.Database.Path)

		database, err := db.Open(cfg.Database.Path)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer database.Close()

		fmt.Println("Database initialized successfully!")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}