package bot

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mistic0xb/zapbot/config"
	"github.com/mistic0xb/zapbot/internal/bunker"
	"github.com/mistic0xb/zapbot/internal/db"
	"github.com/mistic0xb/zapbot/internal/nostrlist"
	reaction "github.com/mistic0xb/zapbot/internal/reactor"
	"github.com/mistic0xb/zapbot/internal/zap"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

type Bot struct {
	config       *config.Config
	db           *db.DB
	pool         *nostr.SimplePool
	zapper       *zap.Zapper
	bunkerClient *bunker.Client
	npubs        []string
	ctx          context.Context
	cancel       context.CancelFunc
}

func New(cfg *config.Config, database *db.DB) (*Bot, error) {
	if cfg.SelectedList == "" {
		return nil, fmt.Errorf("no list selected.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	pool := nostr.NewSimplePool(ctx)

	//  bunker client
	bunkerClient, err := bunker.NewClient(ctx, cfg.Author.BunkerURL, pool)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create bunker client: %w", err)
	}

	// Create zapper
	zapper, err := zap.New(cfg.NWCUrl, cfg.Relays, pool)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create zapper: %w", err)
	}

	return &Bot{
		config:       cfg,
		db:           database,
		pool:         pool,
		zapper:       zapper,
		bunkerClient: bunkerClient,
		ctx:          ctx,
		cancel:       cancel,
	}, nil
}

func (b *Bot) Start() error {
	fmt.Println("Starting Zapbot...")
	fmt.Printf("Selected list: %s\n", b.config.SelectedList)
	fmt.Println()

	// Load NPubs
	if err := b.loadNPubs(); err != nil {
		return fmt.Errorf("failed to load list: %w", err)
	}

	fmt.Printf("Monitoring %d npubs\n", len(b.npubs))
	fmt.Println()

	// Connect to NWC
	fmt.Println("Connecting to wallet...")
	if err := b.zapper.Connect(b.ctx); err != nil {
		return fmt.Errorf("failed to connect to wallet: %w", err)
	}
	defer b.zapper.Close()

	// Check balance
	balance, err := b.zapper.GetBalance(b.ctx)
	if err != nil {
		fmt.Printf("Warning: could not fetch balance: %v\n", err)
	} else {
		fmt.Printf("Wallet balance: %d sats\n", balance/1000)
	}
	fmt.Println()

	// Subscribe to events
	if err := b.subscribeToEvents(); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	<-b.ctx.Done()

	return nil
}

// Stop gracefully shuts down the bot
func (b *Bot) Stop() {
	fmt.Println("\nStopping bot...")
	b.cancel()
}

// loadNPubs loads the NPubs from the selected list
func (b *Bot) loadNPubs() error {
	npubs, err := nostrlist.GetNPubsFromList(
		b.config.Relays,
		b.config.Author.NPub,
		b.bunkerClient,
		b.pool,
		b.config.SelectedList,
	)
	if err != nil {
		return err
	}

	if len(npubs) == 0 {
		return fmt.Errorf("selected list is empty")
	}

	b.npubs = npubs

	// Display who we're monitoring
	fmt.Println("Monitoring these npubs:")
	for i, npub := range b.npubs {
		fmt.Printf("  %d. %s\n", i+1, npub)
	}

	return nil
}

// subscribeToEvents subscribes to kind 1 events from monitored npubs
func (b *Bot) subscribeToEvents() error {
	// Convert npubs to hex pubkeys for filter
	pubkeys, err := b.npubsToHex()
	if err != nil {
		return err
	}

	// Create filter for kind 1 (text notes) from our npubs
	// Start from now (only new events)
	since := nostr.Now()

	filters := []nostr.Filter{{
		Kinds:   []int{1}, // Kind 1 = text notes
		Authors: pubkeys,
		Since:   &since,
	}}

	fmt.Printf("Subscribing to kind 1 events from %d authors...\n", len(pubkeys))
	fmt.Println()

	// Subscribe to events
	go b.handleEvents(filters)

	return nil
}

// handleEvents processes incoming events
func (b *Bot) handleEvents(filters []nostr.Filter) {
	// Subscribe to multiple relays
	for event := range b.pool.SubscribeMany(b.ctx, b.config.Relays, filters[0]) {
		// Process each event
		b.processEvent(event)
	}
}

func (b *Bot) processEvent(event nostr.RelayEvent) {
	if event.Kind != 1 {
		return
	}

	fmt.Printf("\n[%s] New note from %s\n",
		time.Now().Format("15:04:05"),
		event.PubKey[:16]+"...",
	)
	fmt.Printf("Content: %s\n", truncate(event.Content, 80))

	// Check if already zapped
	isZapped, err := b.db.IsZapped(event.ID)
	if err != nil {
		fmt.Printf("Error checking zap status: %v\n", err)
		return
	}

	if isZapped {
		fmt.Println("Already zapped. Skipping.")
		return
	}

	// Check daily budget
	todayTotal, err := b.db.GetTodayTotal()
	if err != nil {
		fmt.Printf("Error checking budget: %v\n", err)
		return
	}

	if todayTotal+b.config.Zap.Amount > b.config.Budget.DailyLimit {
		fmt.Printf("⚠️  Daily budget exceeded (%d/%d sats)\n",
			todayTotal, b.config.Budget.DailyLimit)
		return
	}

	// Check per-npub budget
	authorTotal, err := b.db.GetTodayTotalForAuthor(event.PubKey)
	if err != nil {
		fmt.Printf("Error checking author budget: %v\n", err)
		return
	}

	if authorTotal+b.config.Zap.Amount > b.config.Budget.PerNPubLimit {
		fmt.Printf("⚠️  Per-author budget exceeded for %s (%d/%d sats)\n",
			event.PubKey[:16]+"...",
			authorTotal,
			b.config.Budget.PerNPubLimit,
		)
		return
	}

	// Send the zap!
	fmt.Printf("🌩️  Zapping %d sats...\n", b.config.Zap.Amount)

	zapCtx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()

	err = b.zapper.ZapNote(zapCtx, event.ID, event.PubKey, b.config.Zap.Amount, b.config.Zap.Comment, b.bunkerClient)
	if err != nil {
		fmt.Printf("Zap failed: %v\n", err)
		return
	}

	// Mark as zapped
	err = b.db.MarkZapped(event.ID, event.PubKey, b.config.Zap.Amount, int64(event.CreatedAt))
	if err != nil {
		fmt.Printf("Warning: failed to mark as zapped: %v\n", err)
	}

	fmt.Printf("✅ Zapped successfully!\n")

	// React to the note
	if b.config.Reaction.Enabled {
		reactCtx, reactCancel := context.WithTimeout(b.ctx, 10*time.Second)
		defer reactCancel()

		err = reaction.React(reactCtx, event.ID, event.PubKey, &b.config.Reaction, b.bunkerClient, b.config.Relays)
		if err != nil {
			fmt.Printf("Reaction failed: %v\n", err)
			// Don't return - still mark as zapped
		} else {
			fmt.Printf("💬 Reacted successfully!\n")
		}
	}
}

// npubsToHex converts npubs to hex pubkeys
func (b *Bot) npubsToHex() ([]string, error) {
	pubkeys := make([]string, 0, len(b.npubs))

	for _, npub := range b.npubs {
		hr, data, err := nip19.Decode(npub)
		if err != nil {
			return nil, fmt.Errorf("failed to decode %s: %w", npub, err)
		}

		if hr != "npub" {
			return nil, fmt.Errorf("expected npub, got %s", hr)
		}

		// Handle both string and []byte types
		var hexPubkey string
		switch v := data.(type) {
		case string:
			hexPubkey = v
		case []byte:
			hexPubkey = hex.EncodeToString(v)
		default:
			return nil, fmt.Errorf("unexpected type from decode: %T", data)
		}

		pubkeys = append(pubkeys, hexPubkey)
	}

	return pubkeys, nil
}

// truncate truncates a string to maxLen
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
