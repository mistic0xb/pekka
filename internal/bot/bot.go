package bot

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/mistic0xb/pekka/config"
	"github.com/mistic0xb/pekka/internal/bunker"
	"github.com/mistic0xb/pekka/internal/db"
	"github.com/mistic0xb/pekka/internal/logger"
	"github.com/mistic0xb/pekka/internal/nostrlist"
	reaction "github.com/mistic0xb/pekka/internal/reactor"
	"github.com/mistic0xb/pekka/internal/ui"
	"github.com/mistic0xb/pekka/internal/zap"

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
	logger.Log.Info().Msg("initializing bot")

	if cfg.SelectedList == "" {
		logger.Log.Error().Msg("no selected list in config")
		return nil, fmt.Errorf("no list selected.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	pool := nostr.NewSimplePool(ctx)

	bunkerClient, err := bunker.NewClient(ctx, cfg.Author.BunkerURL, pool)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to create bunker client")
		cancel()
		return nil, fmt.Errorf("failed to create bunker client: %w", err)
	}

	zapper, err := zap.New(cfg.NWCUrl, cfg.Relays, pool)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to create zapper")
		cancel()
		return nil, fmt.Errorf("failed to create zapper: %w", err)
	}

	logger.Log.Info().Msg("bot initialized successfully")

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
	logger.Log.Info().Str("list_id", b.config.SelectedList).Msg("starting bot")

	fmt.Println("Starting Pekka 🟪")
	fmt.Printf("Selected list: %s\n", b.config.SelectedList)
	fmt.Println()

	if err := b.loadNPubs(); err != nil {
		logger.Log.Error().Err(err).Msg("failed to load npubs")
		return fmt.Errorf("failed to load list: %w", err)
	}

	logger.Log.Info().Int("npub_count", len(b.npubs)).Msg("loaded npubs")
	fmt.Println()
	fmt.Printf("Monitoring %d npubs\n", len(b.npubs))
	fmt.Println()

	s := ui.NewSpinner("Connecting to wallet", 11, "yellow")
	if err := b.zapper.Connect(b.ctx); err != nil {
		logger.Log.Error().Err(err).Msg("failed to connect to wallet")
		return fmt.Errorf("failed to connect to wallet: %w", err)
	}
	defer b.zapper.Close()
	s.Stop()

	balance, err := b.zapper.GetBalance(b.ctx)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to fetch wallet balance")
		fmt.Printf("Warning: could not fetch balance: %v\n", err)
	} else {
		logger.Log.Info().Int64("balance_msat", balance).Msg("wallet balance fetched")
		fmt.Println()
		fmt.Printf("Wallet balance: %d sats\n", balance/1000)
	}
	fmt.Println()

	s = ui.NewSpinner("Subscribing to events", 11, "blue")
	if err := b.subscribeToEvents(); err != nil {
		logger.Log.Error().Err(err).Msg("failed to subscribe to events")
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	s.Stop()

	logger.Log.Info().Msg("bot is running")
	fmt.Println("Pekka 🤖 is running. Press Ctrl+C to stop.")
	<-b.ctx.Done()

	logger.Log.Info().Msg("bot context cancelled")
	return nil
}

func (b *Bot) Stop() {
	logger.Log.Info().Msg("stopping bot")
	fmt.Println("\nStopping bot...")
	b.cancel()
}

func (b *Bot) loadNPubs() error {
	logger.Log.Info().Str("list_id", b.config.SelectedList).Msg("loading npubs from list")

	npubs, err := nostrlist.GetNPubsFromList(
		b.config.Relays,
		b.config.Author.NPub,
		b.bunkerClient,
		b.pool,
		b.config.SelectedList,
	)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to fetch npubs from list")
		return err
	}

	if len(npubs) == 0 {
		logger.Log.Error().Msg("selected list is empty")
		return fmt.Errorf("selected list is empty")
	}

	b.npubs = npubs

	fmt.Println("Monitoring these npubs:")
	for i, npub := range b.npubs {
		fmt.Printf("  %d. %s\n", i+1, npub)
	}

	return nil
}

func (b *Bot) subscribeToEvents() error {
	pubkeys, err := b.npubsToHex()
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to convert npubs to hex")
		return err
	}

	since := nostr.Now()
	filters := []nostr.Filter{{
		Kinds:   []int{1},
		Authors: pubkeys,
		Since:   &since,
	}}

	logger.Log.Info().Int("author_count", len(pubkeys)).Msg("subscribing to events")
	go b.handleEvents(filters)
	return nil
}

func (b *Bot) handleEvents(filters []nostr.Filter) {
	for event := range b.pool.SubscribeMany(b.ctx, b.config.Relays, filters[0]) {
		b.processEvent(event)
	}
}

func (b *Bot) processEvent(event nostr.RelayEvent) {
	if event.Kind != 1 {
		return
	}

	logger.Log.Info().
		Str("event_id", event.ID).
		Str("author", event.PubKey).
		Msg("new note received")

	fmt.Printf("\n[%s] New note from %s\n",
		time.Now().Format("15:04:05"),
		event.PubKey[:16]+"...",
	)
	fmt.Printf("Content: %s\n", truncate(event.Content, 80))

	// Check if already zapped
	isZapped, err := b.db.IsZapped(event.ID)
	if err != nil {
		logger.Log.Error().Err(err).Str("event_id", event.ID).Msg("failed to check zap status")
		fmt.Printf("Error checking zap status: %v\n", err)
		return
	}

	if isZapped {
		logger.Log.Info().Str("event_id", event.ID).Msg("event already zapped")
		fmt.Println("Already zapped. Skipping.")
		return
	}

	// Check daily budget
	todayTotal, err := b.db.GetTodayTotal()
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to fetch daily total")
		fmt.Printf("Error checking budget: %v\n", err)
		return
	}

	if todayTotal+b.config.Zap.Amount > b.config.Budget.DailyLimit {
		logger.Log.Info().
			Int("today_total", todayTotal).
			Int("limit", b.config.Budget.DailyLimit).
			Msg("daily budget exceeded")
		fmt.Printf("⚠️  Daily budget exceeded (%d/%d sats)\n", todayTotal, b.config.Budget.DailyLimit)
		return
	}

	// Check per-author budget
	authorTotal, err := b.db.GetTodayTotalForAuthor(event.PubKey)
	if err != nil {
		logger.Log.Error().Err(err).Str("author", event.PubKey).Msg("failed to fetch author budget")
		fmt.Printf("Error checking author budget: %v\n", err)
		return
	}

	if authorTotal+b.config.Zap.Amount > b.config.Budget.PerNPubLimit {
		logger.Log.Info().
			Str("author", event.PubKey).
			Int("author_total", authorTotal).
			Msg("per-author budget exceeded")
		fmt.Printf("⚠️  Per-author budget exceeded for %s (%d/%d sats)\n",
			event.PubKey[:16]+"...", authorTotal, b.config.Budget.PerNPubLimit)
		return
	}

	fmt.Printf("🌩️  Zapping %d sats", b.config.Zap.Amount)
	if b.config.Reaction.Enabled {
		fmt.Printf(" and reacting with %s", b.config.Reaction.Content)
	}
	fmt.Println()

	var wg sync.WaitGroup
	var zapSuccess, reactSuccess bool

	// Launch zap in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		zapSuccess = b.tryZap(event)
	}()

	// Launch reaction in goroutine (if enabled)
	if b.config.Reaction.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reactSuccess = b.tryReact(event)
		}()
	}

	// Wait for both to complete
	wg.Wait()

	if zapSuccess {
		fmt.Printf("✅ Zapped successfully!\n")

		// Mark as zapped in database
		err = b.db.MarkZapped(event.ID, event.PubKey, b.config.Zap.Amount, int64(event.CreatedAt))
		if err != nil {
			logger.Log.Error().Err(err).Str("event_id", event.ID).Msg("failed to mark zap in database")
			fmt.Printf("⚠️  Warning: failed to mark as zapped: %v\n", err)
		}
	} else {
		fmt.Printf("❌ Zap failed after retry. Skipping.\n")
		// Don't mark as zapped - retry
	}

	if b.config.Reaction.Enabled {
		if reactSuccess {
			fmt.Printf("💬 Reacted successfully!\n")
		} else {
			fmt.Printf("⚠️  Reaction failed after retry.\n")
			// Continue - zap might have succeeded
		}
	}
}

// tryZap attempts to zap (with 1 retry)
func (b *Bot) tryZap(event nostr.RelayEvent) bool {
	for attempt := 1; attempt <= 2; attempt++ {
		logger.Log.Info().
			Str("event_id", event.ID).
			Int("attempt", attempt).
			Msg("attempting zap")

		zapCtx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		err := b.zapper.ZapNote(
			zapCtx,
			event.ID,
			event.PubKey,
			b.config.Zap.Amount,
			b.config.Zap.Comment,
			b.bunkerClient,
		)
		cancel()

		if err == nil {
			logger.Log.Info().
				Str("event_id", event.ID).
				Int("attempt", attempt).
				Msg("zap successful")
			return true
		}

		logger.Log.Error().
			Err(err).
			Str("event_id", event.ID).
			Int("attempt", attempt).
			Msg("zap failed")

		if attempt == 1 {
			fmt.Printf("⚠️  Zap failed, retrying...\n")
			time.Sleep(2 * time.Second) // Brief pause before retry
		}
	}

	logger.Log.Error().
		Str("event_id", event.ID).
		Msg("zap failed after 2 attempts")
	return false
}

// tryReact attempts to react (with 1 retry)
func (b *Bot) tryReact(event nostr.RelayEvent) bool {
	for attempt := 1; attempt <= 2; attempt++ {
		logger.Log.Info().
			Str("event_id", event.ID).
			Str("reaction", b.config.Reaction.Content).
			Int("attempt", attempt).
			Msg("attempting reaction")

		reactCtx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
		err := reaction.React(
			reactCtx,
			event.ID,
			event.PubKey,
			&b.config.Reaction,
			b.bunkerClient,
			b.config.Relays,
		)
		cancel()

		if err == nil {
			logger.Log.Info().
				Str("event_id", event.ID).
				Int("attempt", attempt).
				Msg("reaction successful")
			return true
		}

		logger.Log.Error().
			Err(err).
			Str("event_id", event.ID).
			Int("attempt", attempt).
			Msg("reaction failed")

		if attempt == 1 {
			time.Sleep(1 * time.Second) // Brief pause before retry
		}
	}

	logger.Log.Error().
		Str("event_id", event.ID).
		Msg("reaction failed after 2 attempts")
	return false
}

func (b *Bot) npubsToHex() ([]string, error) {
	pubkeys := make([]string, 0, len(b.npubs))

	for _, npub := range b.npubs {
		hr, data, err := nip19.Decode(npub)
		if err != nil {
			logger.Log.Error().Err(err).Str("npub", npub).Msg("failed to decode npub")
			return nil, fmt.Errorf("failed to decode %s: %w", npub, err)
		}

		if hr != "npub" {
			logger.Log.Error().Str("hr", hr).Msg("unexpected nip19 prefix")
			return nil, fmt.Errorf("expected npub, got %s", hr)
		}

		var hexPubkey string
		switch v := data.(type) {
		case string:
			hexPubkey = v
		case []byte:
			hexPubkey = hex.EncodeToString(v)
		default:
			logger.Log.Error().Msg("unexpected nip19 decode type")
			return nil, fmt.Errorf("unexpected type from decode: %T", data)
		}

		pubkeys = append(pubkeys, hexPubkey)
	}

	return pubkeys, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
