package cmd

import (
	"github.com/spf13/cobra"
)

// showCmd prints the current configuration
var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration",
	Long:  `Prints the loaded configuration with sensitive data masked.`,
	Run:   showConfig,
}

func showConfig(cmd *cobra.Command, args []string) {
	cfg := GetConfig()
	cfg.Print()
}

func init() {
	rootCmd.AddCommand(showCmd)
}
