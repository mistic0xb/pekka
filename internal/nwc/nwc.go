package nwc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mistic0xb/pekka/internal/logger"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
)

type Client struct {
	walletPubkey string
	secret       string
	relay        *nostr.Relay
	relayURL     string
}

// Request represents a NIP-47 request
type Request struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// Response represents a NIP-47 response
type Response struct {
	ResultType string                 `json:"result_type"`
	Result     map[string]interface{} `json:"result,omitempty"`
	Error      *ResponseError         `json:"error,omitempty"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewClient creates NWC client from nostr+walletconnect:// URL
func NewClient(nwcURL string) (*Client, error) {
	u, err := url.Parse(nwcURL)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("invalid NWC URL")
		return nil, fmt.Errorf("invalid NWC URL: %w", err)
	}

	if u.Scheme != "nostr+walletconnect" {
		logger.Log.Error().
			Str("scheme", u.Scheme).
			Msg("invalid NWC URL scheme")
		return nil, fmt.Errorf("invalid scheme: expected nostr+walletconnect, got %s", u.Scheme)
	}

	walletPubkey := u.Host
	query := u.Query()
	relayURL := query.Get("relay")
	secret := query.Get("secret")

	if relayURL == "" {
		logger.Log.Error().
			Msg("missing relay parameter in NWC URL")
		return nil, fmt.Errorf("missing relay parameter")
	}

	if secret == "" {
		logger.Log.Error().
			Msg("missing secret parameter in NWC URL")
		return nil, fmt.Errorf("missing secret parameter")
	}

	logger.Log.Info().
		Msg("NWC client created")

	return &Client{
		walletPubkey: walletPubkey,
		secret:       secret,
		relayURL:     relayURL,
	}, nil
}

// Connect establishes connection to wallet relay
func (c *Client) Connect(ctx context.Context) error {
	relay, err := nostr.RelayConnect(ctx, c.relayURL)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("relay", c.relayURL).
			Msg("failed to connect to wallet relay")
		return fmt.Errorf("failed to connect to %s: %w", c.relayURL, err)
	}

	c.relay = relay

	logger.Log.Info().
		Str("relay", c.relayURL).
		Msg("connected to wallet relay")

	return nil
}

// Close closes the relay connection
func (c *Client) Close() error {
	if c.relay != nil {
		logger.Log.Info().
			Msg("closing wallet relay connection")
		return c.relay.Close()
	}
	return nil
}

// PayInvoice pays a lightning invoice
func (c *Client) PayInvoice(ctx context.Context, invoice string) error {
	request := Request{
		Method: "pay_invoice",
		Params: map[string]any{
			"invoice": invoice,
		},
	}

	response, err := c.sendRequest(ctx, request)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("pay_invoice request failed")
		return err
	}

	if response.Error != nil {
		logger.Log.Error().
			Str("code", response.Error.Code).
			Str("message", response.Error.Message).
			Msg("wallet returned payment error")
		return fmt.Errorf("payment failed: %s - %s", response.Error.Code, response.Error.Message)
	}

	logger.Log.Info().
		Msg("invoice paid successfully")

	return nil
}

// GetBalance gets wallet balance in millisats
func (c *Client) GetBalance(ctx context.Context) (int64, error) {
	request := Request{
		Method: "get_balance",
		Params: map[string]any{},
	}

	response, err := c.sendRequest(ctx, request)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("get_balance request failed")
		return 0, err
	}

	if response.Error != nil {
		logger.Log.Error().
			Str("code", response.Error.Code).
			Str("message", response.Error.Message).
			Msg("wallet returned get_balance error")
		return 0, fmt.Errorf("get_balance failed: %s - %s", response.Error.Code, response.Error.Message)
	}

	balance, ok := response.Result["balance"].(float64)
	if !ok {
		logger.Log.Error().
			Msg("invalid balance type in wallet response")
		return 0, fmt.Errorf("invalid balance in response")
	}

	logger.Log.Info().
		Msg("wallet balance fetched")

	return int64(balance), nil
}

func (c *Client) sendRequest(ctx context.Context, req Request) (*Response, error) {
	if c.relay == nil {
		logger.Log.Error().
			Msg("sendRequest called without relay connection")
		return nil, fmt.Errorf("not connected to relay")
	}

	sharedSecret, err := nip04.ComputeSharedSecret(c.walletPubkey, c.secret)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to compute shared secret")
		return nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	ourPubkey, err := nostr.GetPublicKey(c.secret)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("invalid client secret")
		return nil, fmt.Errorf("invalid secret: %w", err)
	}

	event := nostr.Event{
		PubKey:    ourPubkey,
		CreatedAt: nostr.Now(),
		Kind:      23194,
		Tags:      nostr.Tags{{"p", c.walletPubkey}},
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to marshal NWC request")
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	encrypted, err := nip04.Encrypt(string(reqJSON), sharedSecret)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to encrypt NWC request")
		return nil, fmt.Errorf("failed to encrypt request: %w", err)
	}

	event.Content = encrypted
	event.ID = event.GetID()
	event.Sign(c.secret)

	// retry logic
	for range 3 {
		err = c.relay.Publish(ctx, event)
		if err == nil {
			break
		}

		if strings.Contains(err.Error(), "connection closed") {
			// Reconnect and retry
			time.Sleep(1 * time.Second)
			c.relay, _ = nostr.RelayConnect(ctx, c.relayURL)
			continue
		}

		break
	}

	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to publish NWC request")
		return nil, fmt.Errorf("failed to publish request: %w", err)
	}

	responseCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	filters := []nostr.Filter{{
		Kinds: []int{23195},
		Tags:  nostr.TagMap{"e": []string{event.ID}},
		Limit: 1,
	}}

	sub, err := c.relay.Subscribe(responseCtx, filters)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to subscribe to wallet response")
		return nil, fmt.Errorf("failed to subscribe to response: %w", err)
	}

	select {
	case responseEvent := <-sub.Events:
		decrypted, err := nip04.Decrypt(responseEvent.Content, sharedSecret)
		if err != nil {
			logger.Log.Error().
				Err(err).
				Msg("failed to decrypt wallet response")
			return nil, fmt.Errorf("failed to decrypt response: %w", err)
		}

		var response Response
		if err := json.Unmarshal([]byte(decrypted), &response); err != nil {
			logger.Log.Error().
				Err(err).
				Msg("failed to parse wallet response")
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		return &response, nil

	case <-responseCtx.Done():
		logger.Log.Error().
			Msg("timeout waiting for wallet response")
		return nil, fmt.Errorf("timeout waiting for wallet response")
	}
}
