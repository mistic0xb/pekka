package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/mistic0xb/pekka/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	cfg     *config.Config
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "pekka",
	Short: "Automatically zap events",
	Long:  "A Nostr bot that automatically zaps kind 1 events (text notes) from npubs in your configured list using Nostr Wallet Connect.",
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in current directory
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigType("yml")
		viper.SetConfigName("config")
	}

	// Read in environment variables that match
	viper.SetEnvPrefix("PEKKA")
	viper.AutomaticEnv()

	// Read the config file
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
		os.Exit(1)
	}

	// Unmarshal config into struct
	cfg = &config.Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		log.Fatalf("Error parsing config: %v\n", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v\n", err)
	}

}

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}
