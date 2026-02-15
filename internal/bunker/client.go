package bunker

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mistic0xb/zapbot/internal/ui"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip46"
)

type Client struct {
	bunker *nip46.BunkerClient
}

// NewClient creates a bunker client from bunker:// URL
func NewClient(ctx context.Context, bunkerURL string, pool *nostr.SimplePool) (*Client, error) {
	if !nip46.IsValidBunkerURL(bunkerURL) {
		return nil, fmt.Errorf("invalid bunker URL format")
	}

	clientSecretKey := nostr.GeneratePrivateKey()

	// spinner
	sp := ui.NewSpinner("Authenticating from bunker")
	// Don't use a timeout context here - let it stay open
	bunker, err := nip46.ConnectBunker(ctx, clientSecretKey, bunkerURL, pool, func(url string) {
		fmt.Printf("Auth URL: %s\n", url)
	})
	sp.Stop()

	if err != nil {
		if strings.Contains(err.Error(), "already connected") && bunker != nil {
			fmt.Println("Connection already exists")
			fmt.Println("This is usually fine - continuing anyway...")
			fmt.Println()
			return &Client{bunker: bunker}, nil
		}
		return nil, fmt.Errorf("failed to connect to bunker: %w", err)
	}

	fmt.Println("Connected to bunker successfully!")
	fmt.Println()

	return &Client{bunker: bunker}, nil
}

// DecryptNIP44 decrypts content using NIP-44 with detailed logging
func (c *Client) DecryptNIP44(ctx context.Context, senderPubkey, ciphertext string) (string, error) {
	// Add a reasonable timeout
	decryptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := c.bunker.NIP44Decrypt(decryptCtx, senderPubkey, ciphertext)
	if err != nil {
		log.Fatalf("Bunker returned error: %v\n", err)
		return "", err
	}

	return result, nil
}

// DecryptNIP04 decrypts content using NIP-04 with detailed logging
func (c *Client) DecryptNIP04(ctx context.Context, senderPubkey, ciphertext string) (string, error) {
	fmt.Printf("Sending NIP-04 decrypt request to bunker...\n")
	decryptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := c.bunker.NIP04Decrypt(decryptCtx, senderPubkey, ciphertext)
	if err != nil {
		log.Fatalf("Bunker returned error: %v\n", err)
		return "", err
	}

	return result, nil
}

// GetPublicKey gets the bunker's public key
func (c *Client) GetPublicKey(ctx context.Context) (string, error) {
	return c.bunker.GetPublicKey(ctx)
}

// SignEvent signs an event using the remote signer
func (c *Client) SignEvent(ctx context.Context, event *nostr.Event) error {
	return c.bunker.SignEvent(ctx, event)
}
