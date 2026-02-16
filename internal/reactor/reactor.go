package reaction

import (
	"context"
	"fmt"

	"github.com/mistic0xb/pekka/config"
	"github.com/mistic0xb/pekka/internal/bunker"
	"github.com/nbd-wtf/go-nostr"
)

// React creates and publishes a reaction (kind 7) to an event
func React(ctx context.Context, eventID, authorPubkey string, cfg *config.ReactionConfig, bunkerClient *bunker.Client, relays []string) error {
	if !cfg.Enabled {
		return nil // Reactions disabled
	}

	// Get our pubkey from bunker
	ourPubkey, err := bunkerClient.GetPublicKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pubkey: %w", err)
	}

	// Create reaction event (kind 7)
	reaction := nostr.Event{
		PubKey:    ourPubkey,
		CreatedAt: nostr.Now(),
		Kind:      7,
		Tags: nostr.Tags{
			{"e", eventID},      // Event being reacted to
			{"p", authorPubkey}, // Author of the event
			{"k", "1"},          // Kind of event being reacted to
		},
		Content: cfg.Content, //":catJAM:" or "🔥"
	}

	// Add custom emoji tag if provided
	if cfg.EmojiName != "" && cfg.EmojiURL != "" {
		reaction.Tags = append(reaction.Tags, nostr.Tag{"emoji", cfg.EmojiName, cfg.EmojiURL})
	}

	// Calculate event ID
	reaction.ID = reaction.GetID()

	// Sign with bunker
	if err := bunkerClient.SignEvent(ctx, &reaction); err != nil {
		return fmt.Errorf("failed to sign reaction: %w", err)
	}

	// Publish to relays
	publishedCount := 0
	for _, relayURL := range relays {
		relay, err := nostr.RelayConnect(ctx, relayURL)
		if err != nil {
			continue
		}

		if err := relay.Publish(ctx, reaction); err == nil {
			publishedCount++
		}

		relay.Close()
	}

	if publishedCount == 0 {
		return fmt.Errorf("failed to publish reaction to any relay")
	}

	return nil
}
