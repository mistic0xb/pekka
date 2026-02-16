package config

import (
	"fmt"
)

// Config holds all bot configuration
type Config struct {
	Author       AuthorConfig   `mapstructure:"author"`
	Relays       []string       `mapstructure:"relays"`
	SelectedList string         `mapstructure:"selected_list"`
	NWCUrl       string         `mapstructure:"nwc_url"`
	Zap          ZapConfig      `mapstructure:"zap"`
	Reaction     ReactionConfig `mapstructure:"reaction"`
	Budget       BudgetConfig   `mapstructure:"budget"`
	Database     DatabaseConfig `mapstructure:"database"`
}

// Reaction configuration
type ReactionConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Content   string `mapstructure:"content"`    // The emoji/reaction text (e.g., ":catJAM:" or "🔥")
	EmojiName string `mapstructure:"emoji_name"` // Optional custom emoji name
	EmojiURL  string `mapstructure:"emoji_url"`  // Optional custom emoji URL (gif/image)
}

type AuthorConfig struct {
	NPub      string `mapstructure:"npub"`
	BunkerURL string `mapstructure:"bunker_url"` // Changed from NSec

}

type ZapConfig struct {
	Amount  int    `mapstructure:"amount"`
	Comment string `mapstructure:"comment"`
}

type BudgetConfig struct {
	DailyLimit   int `mapstructure:"daily_limit"`
	PerNPubLimit int `mapstructure:"per_npub_limit"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

// Validate checks if config is valid
func (c *Config) Validate() error {
	if c.Author.NPub == "" {
		return fmt.Errorf("author.npub is required")
	}

	if c.Author.BunkerURL == "" {
		return fmt.Errorf("author.bunker_url is required")
	}

	if len(c.Relays) == 0 {
		return fmt.Errorf("at least one relay is required")
	}
	if len(c.Relays) == 0 {
		return fmt.Errorf("at least one relay is required")
	}

	if c.NWCUrl == "" {
		return fmt.Errorf("nwc_url is required")
	}

	if c.Zap.Amount <= 0 {
		return fmt.Errorf("zap amount must be positive")
	}

	if c.Reaction.Enabled {
		if c.Reaction.Content == "" {
			return fmt.Errorf("reaction.content is required when reactions are enabled")
		}

		// If custom emoji is provided, both name and URL are required
		if (c.Reaction.EmojiName != "" && c.Reaction.EmojiURL == "") ||
			(c.Reaction.EmojiName == "" && c.Reaction.EmojiURL != "") {
			return fmt.Errorf("both reaction.emoji_name and reaction.emoji_url must be provided together")
		}
	}

	if c.Budget.DailyLimit <= 0 {
		return fmt.Errorf("daily budget limit must be positive")
	}

	if c.Database.Path == "" {
		return fmt.Errorf("database path is required")
	}

	return nil
}

// Print displays the config (for debugging)
func (c *Config) Print() {
	fmt.Println("=== Zap Bot Configuration ===")
	fmt.Println()

	fmt.Printf("Author Npub: %s\n", c.Author.NPub)
	fmt.Println()

	if c.SelectedList != "" {
		fmt.Printf("Selected List: %s\n", c.SelectedList)
	} 

	fmt.Println("Relays:")
	for i, relay := range c.Relays {
		fmt.Printf("  %v %s\n", i+1, relay)
	}
	fmt.Println()

	fmt.Printf("Zap Amount: %d sats\n", c.Zap.Amount)
	fmt.Println()

	fmt.Printf("Daily Budget Limit: %d sats\n", c.Budget.DailyLimit)
	fmt.Printf("Per-NPub Limit: %d sats\n", c.Budget.PerNPubLimit)
	fmt.Println()

	fmt.Printf("Database Path: %s\n", c.Database.Path)
	fmt.Println()
	fmt.Println("===================================")
}

// maskNWCUrl masks the NWC URL for security (show only first/last few chars)
func maskNWCUrl(url string) string {
	if len(url) <= 30 {
		return "**1"
	}
	return url[:15] + "..." + url[len(url)-32:]
}
