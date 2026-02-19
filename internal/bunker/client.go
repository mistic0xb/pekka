package bunker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mistic0xb/pekka/internal/logger"
	"github.com/mistic0xb/pekka/internal/ui"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip46"
)

type Client struct {
	bunker *nip46.BunkerClient
}

// loadOrCreateClientKey loads a persisted ephemeral key, or creates and saves a new one.
// Reusing the same client key across runs means Amber/remote signers remember the
// granted permissions and don't require re-approval every time.
func loadOrCreateClientKey() (string, error) {
	keyPath := ".bunker_client_key" // saved beside config.yml in the root directory

	data, err := os.ReadFile(keyPath)
	if err == nil {
		key := strings.TrimSpace(string(data))
		if len(key) == 64 {
			logger.Log.Info().Str("key_path", keyPath).Msg("loaded persisted client key")
			return key, nil
		}
		logger.Log.Warn().Str("key_path", keyPath).Msg("persisted key invalid, regenerating")
	}

	key := nostr.GeneratePrivateKey()
	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		logger.Log.Warn().Err(err).Msg("could not persist client key; permissions will reset on next run")
	} else {
		logger.Log.Info().Str("key_path", keyPath).Msg("generated and persisted new client key (beside config.yml)")
	}
	return key, nil
}

// NewClient creates a bunker client from bunkerURL
func NewClient(ctx context.Context, bunkerURL string, pool *nostr.SimplePool) (*Client, error) {
	logger.Log.Info().Msg("validating bunker URL")

	if !nip46.IsValidBunkerURL(bunkerURL) {
		logger.Log.Error().Msg("invalid bunker URL format")
		return nil, fmt.Errorf("invalid bunker URL format")
	}

	clientSecretKey, err := loadOrCreateClientKey()
	if err != nil {
		return nil, fmt.Errorf("could not obtain client key: %w", err)
	}

	sp := ui.NewSpinner("Authenticating from bunker", 11, "blue")

	// Background context — ConnectBunker keeps a relay subscription open for
	// the entire process lifetime. Cancelling this would break all future
	// SignEvent / Decrypt calls.
	bunkerCtx := context.Background()

	logger.Log.Info().Msg("calling ConnectBunker — waiting for remote signer approval")

	bunker, err := nip46.ConnectBunker(bunkerCtx, clientSecretKey, bunkerURL, pool, func(url string) {
		logger.Log.Info().Str("auth_url", url).Msg("bunker auth URL received — open this to approve")
		fmt.Printf("Auth URL: %s\n", url)
	})
	sp.Stop()

	if err != nil {
		if strings.Contains(err.Error(), "already connected") && bunker != nil {
			logger.Log.Warn().Msg("bunker reported already connected — reusing existing connection")
			fmt.Println("Connection already exists, continuing...")
			fmt.Println()
			return &Client{bunker: bunker}, nil
		}
		logger.Log.Error().Err(err).Msg("ConnectBunker failed")
		return nil, fmt.Errorf("failed to connect to bunker: %w", err)
	}

	logger.Log.Info().Msg("bunker connected successfully")
	fmt.Println("Connected to bunker successfully!")
	fmt.Println()
	return &Client{bunker: bunker}, nil
}

// DecryptNIP44 decrypts content using NIP-44
func (c *Client) DecryptNIP44(ctx context.Context, senderPubkey, ciphertext string) (string, error) {
	logger.Log.Debug().Str("sender", senderPubkey).Msg("sending NIP-44 decrypt request to bunker")

	decryptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := c.bunker.NIP44Decrypt(decryptCtx, senderPubkey, ciphertext)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("sender", senderPubkey).
			Bool("context_deadline_exceeded", ctx.Err() == context.DeadlineExceeded).
			Msg("NIP-44 decrypt failed")
		return "", fmt.Errorf("NIP44 decrypt: %w", err)
	}

	logger.Log.Debug().Str("sender", senderPubkey).Msg("NIP-44 decrypt succeeded")
	return result, nil
}

// DecryptNIP04 decrypts content using NIP-04
func (c *Client) DecryptNIP04(ctx context.Context, senderPubkey, ciphertext string) (string, error) {
	logger.Log.Debug().Str("sender", senderPubkey).Msg("sending NIP-04 decrypt request to bunker")

	decryptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := c.bunker.NIP04Decrypt(decryptCtx, senderPubkey, ciphertext)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("sender", senderPubkey).
			Bool("context_deadline_exceeded", ctx.Err() == context.DeadlineExceeded).
			Msg("NIP-04 decrypt failed")
		return "", fmt.Errorf("NIP04 decrypt: %w", err)
	}

	logger.Log.Debug().Str("sender", senderPubkey).Msg("NIP-04 decrypt succeeded")
	return result, nil
}

// GetPublicKey gets the bunker's public key
func (c *Client) GetPublicKey(ctx context.Context) (string, error) {
	logger.Log.Debug().Msg("requesting public key from bunker")

	getPkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pubkey, err := c.bunker.GetPublicKey(getPkCtx)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get public key from bunker")
		return "", err
	}

	logger.Log.Info().Str("pubkey", pubkey).Msg("got public key from bunker")
	return pubkey, nil
}

// SignEvent signs an event using the remote signer
func (c *Client) SignEvent(ctx context.Context, event *nostr.Event) error {
	logger.Log.Debug().
		Str("event_id", event.ID).
		Int("kind", event.Kind).
		Msg("sending sign request to bunker")

	signCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.bunker.SignEvent(signCtx, event); err != nil {
		logger.Log.Error().
			Err(err).
			Str("event_id", event.ID).
			Int("kind", event.Kind).
			Msg("bunker SignEvent failed")
		return err
	}

	logger.Log.Debug().Str("event_id", event.ID).Msg("event signed successfully")
	return nil
}
