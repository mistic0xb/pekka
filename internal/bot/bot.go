package bot

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mistic0xb/zapbot/config"
	"github.com/mistic0xb/zapbot/internal/bunker"
	"github.com/mistic0xb/zapbot/internal/db"
	"github.com/mistic0xb/zapbot/internal/logger"
	"github.com/mistic0xb/zapbot/internal/nostrlist"
	reaction "github.com/mistic0xb/zapbot/internal/reactor"
	"github.com/mistic0xb/zapbot/internal/ui"
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
	logger.Log.Info().
		Msg("initializing bot")

	if cfg.SelectedList == "" {
		logger.Log.Error().
			Msg("no selected list in config")
		return nil, fmt.Errorf("no list selected.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	pool := nostr.NewSimplePool(ctx)

	bunkerClient, err := bunker.NewClient(ctx, cfg.Author.BunkerURL, pool)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to create bunker client")
		cancel()
		return nil, fmt.Errorf("failed to create bunker client: %w", err)
	}

	zapper, err := zap.New(cfg.NWCUrl, cfg.Relays, pool)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to create zapper")
		cancel()
		return nil, fmt.Errorf("failed to create zapper: %w", err)
	}

	logger.Log.Info().
		Msg("bot initialized successfully")

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
	logger.Log.Info().
		Str("list_id", b.config.SelectedList).
		Msg("starting bot")

	s := ui.NewSpinner("Starting Zapbot", 11, "green")
	fmt.Printf("Selected list: %s\n", b.config.SelectedList)
	fmt.Println()

	if err := b.loadNPubs(); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to load npubs")
		return fmt.Errorf("failed to load list: %w", err)
	}
	s.Stop()

	logger.Log.Info().
		Int("npub_count", len(b.npubs)).
		Msg("loaded npubs")

	fmt.Printf("Monitoring %d npubs\n", len(b.npubs))
	fmt.Println()

	s = ui.NewSpinner("Connecting to wallet", 11, "yellow")
	if err := b.zapper.Connect(b.ctx); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to connect to wallet")
		return fmt.Errorf("failed to connect to wallet: %w", err)
	}
	defer b.zapper.Close()
	s.Stop()

	balance, err := b.zapper.GetBalance(b.ctx)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to fetch wallet balance")
		fmt.Printf("Warning: could not fetch balance: %v\n", err)
	} else {
		logger.Log.Info().
			Int64("balance_msat", balance).
			Msg("wallet balance fetched")
		fmt.Println()
		fmt.Printf("Wallet balance: %d sats\n", balance/1000)
	}
	fmt.Println()

	if err := b.subscribeToEvents(); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to subscribe to events")
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	logger.Log.Info().
		Msg("bot is running")

	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	<-b.ctx.Done()

	logger.Log.Info().
		Msg("bot context cancelled")

	return nil
}

func (b *Bot) Stop() {
	logger.Log.Info().
		Msg("stopping bot")
	fmt.Println("\nStopping bot...")
	b.cancel()
}

func (b *Bot) loadNPubs() error {
	logger.Log.Info().
		Str("list_id", b.config.SelectedList).
		Msg("loading npubs from list")

	npubs, err := nostrlist.GetNPubsFromList(
		b.config.Relays,
		b.config.Author.NPub,
		b.bunkerClient,
		b.pool,
		b.config.SelectedList,
	)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to fetch npubs from list")
		return err
	}

	if len(npubs) == 0 {
		logger.Log.Error().
			Msg("selected list is empty")
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
		logger.Log.Error().
			Err(err).
			Msg("failed to convert npubs to hex")
		return err
	}

	since := nostr.Now()

	filters := []nostr.Filter{{
		Kinds:   []int{1},
		Authors: pubkeys,
		Since:   &since,
	}}

	logger.Log.Info().
		Int("author_count", len(pubkeys)).
		Msg("subscribing to events")

	fmt.Printf("Subscribing to kind 1 events from %d authors...\n", len(pubkeys))
	fmt.Println()

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

	isZapped, err := b.db.IsZapped(event.ID)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("event_id", event.ID).
			Msg("failed to check zap status")
		fmt.Printf("Error checking zap status: %v\n", err)
		return
	}

	if isZapped {
		logger.Log.Info().
			Str("event_id", event.ID).
			Msg("event already zapped")
		fmt.Println("Already zapped. Skipping.")
		return
	}

	todayTotal, err := b.db.GetTodayTotal()
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to fetch daily total")
		fmt.Printf("Error checking budget: %v\n", err)
		return
	}

	if todayTotal+b.config.Zap.Amount > b.config.Budget.DailyLimit {
		logger.Log.Info().
			Int("today_total", todayTotal).
			Int("limit", b.config.Budget.DailyLimit).
			Msg("daily budget exceeded")
		fmt.Printf("⚠️  Daily budget exceeded (%d/%d sats)\n",
			todayTotal, b.config.Budget.DailyLimit)
		return
	}

	authorTotal, err := b.db.GetTodayTotalForAuthor(event.PubKey)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("author", event.PubKey).
			Msg("failed to fetch author budget")
		fmt.Printf("Error checking author budget: %v\n", err)
		return
	}

	if authorTotal+b.config.Zap.Amount > b.config.Budget.PerNPubLimit {
		logger.Log.Info().
			Str("author", event.PubKey).
			Int("author_total", authorTotal).
			Msg("per-author budget exceeded")
		fmt.Printf("⚠️  Per-author budget exceeded for %s (%d/%d sats)\n",
			event.PubKey[:16]+"...",
			authorTotal,
			b.config.Budget.PerNPubLimit,
		)
		return
	}

	logger.Log.Info().
		Str("event_id", event.ID).
		Int("amount", b.config.Zap.Amount).
		Msg("sending zap")

	fmt.Printf("🌩️  Zapping %d sats...\n", b.config.Zap.Amount)

	zapCtx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()

	err = b.zapper.ZapNote(
		zapCtx,
		event.ID,
		event.PubKey,
		b.config.Zap.Amount,
		b.config.Zap.Comment,
		b.bunkerClient,
	)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("event_id", event.ID).
			Msg("zap failed")
		fmt.Printf("Zap failed: %v\n", err)
		return
	}

	err = b.db.MarkZapped(event.ID, event.PubKey, b.config.Zap.Amount, int64(event.CreatedAt))
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("event_id", event.ID).
			Msg("failed to mark zap in database")
		fmt.Printf("Warning: failed to mark as zapped: %v\n", err)
	}

	logger.Log.Info().
		Str("event_id", event.ID).
		Msg("zap successful")

	fmt.Printf("✅ Zapped successfully!\n")

	if b.config.Reaction.Enabled {
		reactCtx, reactCancel := context.WithTimeout(b.ctx, 10*time.Second)
		defer reactCancel()

		err = reaction.React(
			reactCtx,
			event.ID,
			event.PubKey,
			&b.config.Reaction,
			b.bunkerClient,
			b.config.Relays,
		)
		if err != nil {
			logger.Log.Error().
				Err(err).
				Str("event_id", event.ID).
				Msg("reaction failed")
			fmt.Printf("Reaction failed: %v\n", err)
		} else {
			logger.Log.Info().
				Str("event_id", event.ID).
				Msg("reaction successful")
			fmt.Printf("💬 Reacted successfully!\n")
		}
	}
}

func (b *Bot) npubsToHex() ([]string, error) {
	pubkeys := make([]string, 0, len(b.npubs))

	for _, npub := range b.npubs {
		hr, data, err := nip19.Decode(npub)
		if err != nil {
			logger.Log.Error().
				Err(err).
				Str("npub", npub).
				Msg("failed to decode npub")
			return nil, fmt.Errorf("failed to decode %s: %w", npub, err)
		}

		if hr != "npub" {
			logger.Log.Error().
				Str("hr", hr).
				Msg("unexpected nip19 prefix")
			return nil, fmt.Errorf("expected npub, got %s", hr)
		}

		var hexPubkey string
		switch v := data.(type) {
		case string:
			hexPubkey = v
		case []byte:
			hexPubkey = hex.EncodeToString(v)
		default:
			logger.Log.Error().
				Msg("unexpected nip19 decode type")
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
