package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/mistic0xb/pekka/config"
	"github.com/mistic0xb/pekka/internal/bot"
	"github.com/mistic0xb/pekka/internal/bunker"
	"github.com/mistic0xb/pekka/internal/db"
	"github.com/mistic0xb/pekka/internal/logger"
	"github.com/mistic0xb/pekka/internal/nostrlist"
	"github.com/mistic0xb/pekka/internal/ui"

	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start  auto-zap bot",
	Long:  `Fetches your private lists, lets you select one, and starts auto-zapping.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := GetConfig()

		// Print the config file
		fmt.Printf("Using config file: %s\n\n", viper.ConfigFileUsed())
		cfg.Print()

		// Open database
		database, err := db.Open(cfg.Database.Path)
		if err != nil {
			fmt.Printf("Error opening database: %v\n", err)
			logger.Log.Error().
				Err(err).
				Str("db_path", cfg.Database.Path).
				Msg("failed to open database")
			return
		}
		defer database.Close()

		// Check if list is already selected
		if cfg.SelectedList == "" {
			// No list selected, fetch and prompt user
			if err := selectList(cfg); err != nil {
				fmt.Printf("Error selecting list: %v\n", err)
				return
			}
			// Reload config after selection
			cfg = GetConfig()
		} else {
			// List already selected, confirm with user
			fmt.Printf("Currently selected list: %s\n", cfg.SelectedList)
			fmt.Print("Use this list? (y/n): ")

			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))

			if input != "y" && input != "yes" {
				// User wants to change
				if err := selectList(cfg); err != nil {
					fmt.Printf("Error selecting list: %v\n", err)
					return
				}
				cfg = GetConfig()
			}
		}

		fmt.Println()

		// Create bot
		bot, err := bot.New(cfg, database)
		if err != nil {
			fmt.Printf("Error creating bot: %v\n", err)
			return
		}

		// Handle graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigChan
			bot.Stop()
		}()

		// Start bot
		if err := bot.Start(); err != nil {
			fmt.Printf("Bot error: %v\n", err)
		}
	},
}

// selectList fetches lists and prompts user to select one
func selectList(cfg *config.Config) error {

	// Create pool for bunker
	ctx := context.Background()
	pool := nostr.NewSimplePool(ctx)

	// Create bunker client
	bunkerClient, err := bunker.NewClient(ctx, cfg.Author.BunkerURL, pool)
	if err != nil {
		return fmt.Errorf("failed to connect to bunker: %w\nPlease check your bunker_url in config", err)
	}

	// Spinner
	s := ui.NewSpinner("Fetching your private lists from relays", 11, "blue")
	// Fetch lists
	lists, err := nostrlist.FetchPrivateLists(
		cfg.Relays,
		cfg.Author.NPub,
		bunkerClient,
		pool,
	)
	if err != nil {
		return fmt.Errorf("failed to fetch lists: %w", err)
	}
	s.Stop()

	if len(lists) == 0 {
		return fmt.Errorf("no private lists found. Create one in your Nostr client first")
	}

	// Display lists
	fmt.Println("Available private lists:")
	fmt.Println()
	for i, list := range lists {
		privateMarker := ""
		if list.HasPrivate {
			privateMarker = " (private)"
		}

		fmt.Printf("  %d. %s%s (%d people)\n", i+1, list.Title, privateMarker, len(list.NPubs))
	}
	fmt.Println()

	// Ask user to select
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Select a list (1-%d): ", len(lists))
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(lists) {
		return fmt.Errorf("invalid selection")
	}

	selectedList := lists[choice-1]
	cfg.SelectedList = selectedList.ID

	// Update config file
	viper.Set("selected_list", selectedList.ID)
	if err := viper.WriteConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Printf("Selected: %s (%d people)\n", selectedList.Title, len(selectedList.NPubs))

	return nil
}

func init() {
	rootCmd.AddCommand(startCmd)
}
