package cmd

import (
	"fmt"

	"github.com/mistic0xb/pekka/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the current version of Pekka",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("version:", version.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
