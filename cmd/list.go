package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/mistic0xb/zapbot/internal/nostrlist"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List and select a private list to monitor",
	Long:  `Fetches all your private NIP-51 lists and allows you to select one for auto-zapping.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := GetConfig()

		fmt.Println("Fetching your private lists from relays...")
		fmt.Println()

		// Fetch lists
		lists, err := nostrlist.FetchPrivateLists(
			cfg.Relays,
			cfg.Author.NPub,
			cfg.Author.NSec,
		)
		if err != nil {
			fmt.Printf("Error fetching lists: %v\n", err)
			return
		}

		if len(lists) == 0 {
			fmt.Println("No private lists found.")
			fmt.Println("Create a private list in your Nostr client first (kind 30000).")
			return
		}

		// Display lists
		fmt.Println("\nAvailable private lists:")
		fmt.Println()
		for i, list := range lists {
			privateMarker := ""
			if list.HasPrivate {
				privateMarker = " (private)"
			}
			
			fmt.Printf("  %d. %s%s (%d people)\n", i+1, list.Title, privateMarker, len(list.NPubs))
			fmt.Printf("     ID: %s\n", list.ID)
			fmt.Println()
		}

		// Ask user to select
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Select a list (1-%d): ", len(lists))
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		choice, err := strconv.Atoi(input)
		if err != nil || choice < 1 || choice > len(lists) {
			fmt.Println("Invalid selection.")
			return
		}

		selectedList := lists[choice-1]

		// Update config file
		viper.Set("selected_list", selectedList.ID)
		if err := viper.WriteConfig(); err != nil {
			fmt.Printf("Error saving config: %v\n", err)
			return
		}

		fmt.Println()
		fmt.Printf("✓ Selected list: %s\n", selectedList.Title)
		fmt.Printf("  Monitoring %d npubs:\n", len(selectedList.NPubs))
		for i, npub := range selectedList.NPubs {
			fmt.Printf("    %d. %s\n", i+1, npub)
		}
		fmt.Println()
		fmt.Println("Config updated! You can now run: zapbot start")
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}