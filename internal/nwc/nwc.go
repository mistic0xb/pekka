package nwc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

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
	// Parse: nostr+walletconnect://pubkey?relay=wss://relay&secret=hex
	u, err := url.Parse(nwcURL)
	if err != nil {
		return nil, fmt.Errorf("invalid NWC URL: %w", err)
	}

	if u.Scheme != "nostr+walletconnect" {
		return nil, fmt.Errorf("invalid scheme: expected nostr+walletconnect, got %s", u.Scheme)
	}

	// Extract wallet pubkey from host
	walletPubkey := u.Host

	// Extract relay and secret from query params
	query := u.Query()
	relayURL := query.Get("relay")
	secret := query.Get("secret")

	if relayURL == "" {
		return nil, fmt.Errorf("missing relay parameter")
	}

	if secret == "" {
		return nil, fmt.Errorf("missing secret parameter")
	}

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
		return fmt.Errorf("failed to connect to %s: %w", c.relayURL, err)
	}

	c.relay = relay
	return nil
}

// Close closes the relay connection
func (c *Client) Close() error {
	if c.relay != nil {
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
		return err
	}

	if response.Error != nil {
		return fmt.Errorf("payment failed: %s - %s", response.Error.Code, response.Error.Message)
	}

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
		return 0, err
	}

	if response.Error != nil {
		return 0, fmt.Errorf("get_balance failed: %s - %s", response.Error.Code, response.Error.Message)
	}

	// Balance is in millisats
	balance, ok := response.Result["balance"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid balance in response")
	}

	return int64(balance), nil
}

func (c *Client) sendRequest(ctx context.Context, req Request) (*Response, error) {
	if c.relay == nil {
		return nil, fmt.Errorf("not connected to relay")
	}

	// 1. COMPUTE SHARED SECRET (This creates the correct 32-byte key)
	sharedSecret, err := nip04.ComputeSharedSecret(c.walletPubkey, c.secret)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	// Derive our pubkey from secret
	ourPubkey, err := nostr.GetPublicKey(c.secret)
	if err != nil {
		return nil, fmt.Errorf("invalid secret: %w", err)
	}

	// Create event
	event := nostr.Event{
		PubKey:    ourPubkey,
		CreatedAt: nostr.Now(),
		Kind:      23194, // NIP-47 request
		Tags:      nostr.Tags{{"p", c.walletPubkey}},
	}

	// Serialize request to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 2. USE SHARED SECRET FOR ENCRYPTION
	encrypted, err := nip04.Encrypt(string(reqJSON), sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt request: %w", err)
	}

	event.Content = encrypted

	// Sign event
	event.ID = event.GetID()
	event.Sign(c.secret)

	// Publish request
	err = c.relay.Publish(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("failed to publish request: %w", err)
	}

	// Subscribe to response
	responseCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	filters := []nostr.Filter{{
		Kinds: []int{23195}, // NIP-47 response
		Tags:  nostr.TagMap{"e": []string{event.ID}},
		Limit: 1,
	}}

	sub, err := c.relay.Subscribe(responseCtx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to response: %w", err)
	}

	// Wait for response
	select {
	case responseEvent := <-sub.Events:
		// 3. USE SHARED SECRET FOR DECRYPTION
		decrypted, err := nip04.Decrypt(responseEvent.Content, sharedSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt response: %w", err)
		}

		var response Response
		if err := json.Unmarshal([]byte(decrypted), &response); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		return &response, nil

	case <-responseCtx.Done():
		return nil, fmt.Errorf("timeout waiting for wallet response")
	}
}