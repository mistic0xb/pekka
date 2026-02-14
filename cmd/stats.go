package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/mistic0xb/zapbot/internal/db"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show zapping statistics",
	Long:  `Display statistics about zapped events and budget usage.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := GetConfig()

		// Open database
		db, err := db.Open(cfg.Database.Path)
		if err != nil {
			fmt.Printf("Error opening database: %v\n", err)
			return
		}
		defer db.Close()

		// Get stats
		stats, err := db.GetStats()
		if err != nil {
			fmt.Printf("Error getting stats: %v\n", err)
			return
		}

		// Print stats
		fmt.Println("=== Nostr Zap Bot Statistics ===")
		fmt.Println()
		fmt.Printf("Total Events Zapped: %d\n", stats.TotalZapped)
		fmt.Printf("Total Sats Spent (all time): %d\n", stats.TotalSats)
		fmt.Printf("Unique Authors Zapped: %d\n", stats.UniqueAuthors)
		fmt.Println()
		fmt.Printf("Today's Total: %d sats\n", stats.TodayTotal)
		fmt.Printf("Daily Limit: %d sats\n", cfg.Budget.DailyLimit)
		fmt.Printf("Remaining Today: %d sats\n", cfg.Budget.DailyLimit-stats.TodayTotal)
		fmt.Println()

		// Get recent zaps
		recentZaps, err := db.GetRecentZaps(5)
		if err != nil {
			fmt.Printf("Error getting recent zaps: %v\n", err)
			return
		}

		if len(recentZaps) > 0 {
			fmt.Println("Recent Zaps:")
			for i, z := range recentZaps {
				zappedTime := time.Unix(z.ZappedAt, 0)
				fmt.Printf("  %d. %s - %d sats (%s)\n",
					i+1,
					z.AuthorPubkey[:16]+"...",
					z.Amount,
					zappedTime.Format("2006-01-02 15:04:05"),
				)
			}
		} else {
			fmt.Println("No zaps recorded yet.")
		}

		fmt.Println()
		fmt.Println("================================")
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}