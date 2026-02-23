package bunker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mistic0xb/pekka/internal/logger"
	"github.com/nbd-wtf/go-nostr"
)

type ReconnectingClient struct {
	mu        sync.RWMutex
	client    *Client
	bunkerURL string
	pool      *nostr.SimplePool
	botCtx    context.Context
}

func NewReconnectingClient(botCtx context.Context, bunkerURL string, pool *nostr.SimplePool) (*ReconnectingClient, error) {
	client, err := NewClient(botCtx, bunkerURL, pool)
	if err != nil {
		return nil, err
	}

	rc := &ReconnectingClient{
		client:    client,
		bunkerURL: bunkerURL,
		pool:      pool,
		botCtx:    botCtx,
	}

	rc.startKeepalive()
	return rc, nil
}

func (rc *ReconnectingClient) reconnect() error {
	logger.Log.Info().Msg("reconnecting bunker client")
	client, err := NewClient(rc.botCtx, rc.bunkerURL, rc.pool)
	if err != nil {
		logger.Log.Error().Err(err).Msg("bunker reconnect failed")
		return err
	}
	rc.mu.Lock()
	rc.client = client
	rc.mu.Unlock()
	logger.Log.Info().Msg("bunker reconnected successfully")
	return nil
}

func (rc *ReconnectingClient) startKeepalive() {
	go func() {
		ticker := time.NewTicker(4 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-rc.botCtx.Done():
				return
			case <-ticker.C:
				logger.Log.Info().Msg("proactive bunker keepalive reconnect")
				rc.reconnect()
			}
		}
	}()
}

func (rc *ReconnectingClient) getClient() *Client {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.client
}

func isSessionError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// SignEvent - reconnects once on session error
func (rc *ReconnectingClient) SignEvent(ctx context.Context, event *nostr.Event) error {
	err := rc.getClient().SignEvent(ctx, event)
	if err != nil && isSessionError(err) {
		if reconnErr := rc.reconnect(); reconnErr != nil {
			return err 
		}
		return rc.getClient().SignEvent(ctx, event)
	}
	return err
}

func (rc *ReconnectingClient) GetPublicKey(ctx context.Context) (string, error) {
	pubkey, err := rc.getClient().GetPublicKey(ctx)
	if err != nil && isSessionError(err) {
		if reconnErr := rc.reconnect(); reconnErr != nil {
			return "", err
		}
		return rc.getClient().GetPublicKey(ctx)
	}
	return pubkey, err
}

func (rc *ReconnectingClient) DecryptNIP44(ctx context.Context, senderPubkey, ciphertext string) (string, error) {
	result, err := rc.getClient().DecryptNIP44(ctx, senderPubkey, ciphertext)
	if err != nil && isSessionError(err) {
		if reconnErr := rc.reconnect(); reconnErr != nil {
			return "", err
		}
		return rc.getClient().DecryptNIP44(ctx, senderPubkey, ciphertext)
	}
	return result, err
}

func (rc *ReconnectingClient) DecryptNIP04(ctx context.Context, senderPubkey, ciphertext string) (string, error) {
	result, err := rc.getClient().DecryptNIP04(ctx, senderPubkey, ciphertext)
	if err != nil && isSessionError(err) {
		if reconnErr := rc.reconnect(); reconnErr != nil {
			return "", err
		}
		return rc.getClient().DecryptNIP04(ctx, senderPubkey, ciphertext)
	}
	return result, err
}