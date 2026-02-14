package zap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mistic0xb/zapbot/internal/nwc"
	"github.com/nbd-wtf/go-nostr"
)

type Zapper struct {
	nwcClient *nwc.Client
	pool      *nostr.SimplePool
	relays    []string
}

// New creates a new Zapper
func New(nwcURL string, relays []string, pool *nostr.SimplePool) (*Zapper, error) {
	client, err := nwc.NewClient(nwcURL)
	if err != nil {
		return nil, err
	}

	return &Zapper{
		nwcClient: client,
		pool:      pool,
		relays:    relays,
	}, nil
}

// Connect establishes connection to NWC wallet relay
func (z *Zapper) Connect(ctx context.Context) error {
	return z.nwcClient.Connect(ctx)
}

// Close closes NWC connection
func (z *Zapper) Close() {
	z.nwcClient.Close()
}

// ZapNote sends a zap to a note
func (z *Zapper) ZapNote(ctx context.Context, eventID, authorPubkey string, amountSats int, comment string) error {
	// Step 1: Get author's lightning address from profile
	lightningAddress, err := z.getLightningAddress(ctx, authorPubkey)
	if err != nil {
		return fmt.Errorf("failed to get lightning address: %w", err)
	}

	if lightningAddress == "" {
		return fmt.Errorf("author has no lightning address in profile")
	}

	// Step 2: Convert lightning address to LNURL endpoint
	lnurlEndpoint := z.lightningAddressToLNURL(lightningAddress)

	// Step 3: Request invoice
	invoice, err := z.requestInvoice(ctx, lnurlEndpoint, amountSats, comment)
	if err != nil {
		return fmt.Errorf("failed to request invoice: %w", err)
	}

	// Step 4: Pay invoice via NWC
	if err := z.nwcClient.PayInvoice(ctx, invoice); err != nil {
		return fmt.Errorf("failed to pay invoice: %w", err)
	}

	return nil
}

// getLightningAddress fetches the author's lightning address from their profile (kind 0)
func (z *Zapper) getLightningAddress(ctx context.Context, pubkey string) (string, error) {
	filters := []nostr.Filter{{
		Kinds:   []int{0}, // Kind 0 = profile metadata
		Authors: []string{pubkey},
		Limit:   1,
	}}

	profileCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for event := range z.pool.SubManyEose(profileCtx, z.relays, filters) {
		var profile struct {
			LUD16 string `json:"lud16"` // Lightning address (user@domain.com)
			LUD06 string `json:"lud06"` // LNURL (lnurl1...)
		}

		if err := json.Unmarshal([]byte(event.Content), &profile); err != nil {
			continue
		}

		// Prefer lud16 (lightning address) over lud06
		if profile.LUD16 != "" {
			return profile.LUD16, nil
		}

		if profile.LUD06 != "" {
			// TODO: Decode lud06 bech32 to URL if needed
			return profile.LUD06, nil
		}
	}

	return "", fmt.Errorf("no lightning address found in profile")
}

// lightningAddressToLNURL converts user@domain.com to https://domain.com/.well-known/lnurlp/user
func (z *Zapper) lightningAddressToLNURL(address string) string {
	parts := strings.Split(address, "@")
	if len(parts) != 2 {
		return ""
	}
	user := parts[0]
	domain := parts[1]
	return fmt.Sprintf("https://%s/.well-known/lnurlp/%s", domain, user)
}

// requestInvoice requests a lightning invoice from an LNURL endpoint
func (z *Zapper) requestInvoice(ctx context.Context, lnurlEndpoint string, amountSats int, comment string) (string, error) {
	// Step 1: Fetch LNURL metadata
	metadata, err := z.fetchLNURLMetadata(lnurlEndpoint)
	if err != nil {
		return "", err
	}

	// Step 2: Validate amount
	amountMillisats := int64(amountSats * 1000)
	if amountMillisats < metadata.MinSendable {
		return "", fmt.Errorf("amount %d msats below minimum %d msats", amountMillisats, metadata.MinSendable)
	}
	if amountMillisats > metadata.MaxSendable {
		return "", fmt.Errorf("amount %d msats above maximum %d msats", amountMillisats, metadata.MaxSendable)
	}

	// Step 3: Request invoice
	invoice, err := z.fetchInvoice(metadata.Callback, amountMillisats, comment)
	if err != nil {
		return "", err
	}

	return invoice, nil
}

// LNURLPayMetadata represents LNURL-pay metadata
type LNURLPayMetadata struct {
	Callback       string `json:"callback"`
	MinSendable    int64  `json:"minSendable"`
	MaxSendable    int64  `json:"maxSendable"`
	Tag            string `json:"tag"`
	AllowsNostr    bool   `json:"allowsNostr"`
	NostrPubkey    string `json:"nostrPubkey"`
	CommentAllowed int    `json:"commentAllowed"`
}

// fetchLNURLMetadata fetches metadata from LNURL endpoint
func (z *Zapper) fetchLNURLMetadata(endpoint string) (*LNURLPayMetadata, error) {
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch LNURL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LNURL returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var metadata LNURLPayMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	if metadata.Tag != "payRequest" {
		return nil, fmt.Errorf("invalid tag: expected payRequest, got %s", metadata.Tag)
	}

	return &metadata, nil
}

// fetchInvoice requests an invoice from the callback URL
func (z *Zapper) fetchInvoice(callback string, amountMillisats int64, comment string) (string, error) {
	// Build callback URL with parameters
	callbackURL, err := url.Parse(callback)
	if err != nil {
		return "", fmt.Errorf("invalid callback URL: %w", err)
	}

	q := callbackURL.Query()
	q.Set("amount", strconv.FormatInt(amountMillisats, 10))
	if comment != "" {
		q.Set("comment", comment)
	}
	callbackURL.RawQuery = q.Encode()

	// Request invoice
	resp, err := http.Get(callbackURL.String())
	if err != nil {
		return "", fmt.Errorf("failed to request invoice: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("callback returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read invoice response: %w", err)
	}

	var invoiceResponse struct {
		PR     string `json:"pr"`     // Payment request (bolt11 invoice)
		Status string `json:"status"` // Optional status
		Reason string `json:"reason"` // Error reason if status is ERROR
	}

	if err := json.Unmarshal(body, &invoiceResponse); err != nil {
		return "", fmt.Errorf("failed to parse invoice response: %w", err)
	}

	if invoiceResponse.Status == "ERROR" {
		return "", fmt.Errorf("LNURL error: %s", invoiceResponse.Reason)
	}

	if invoiceResponse.PR == "" {
		return "", fmt.Errorf("no invoice in response")
	}

	return invoiceResponse.PR, nil
}

// GetBalance gets NWC wallet balance in millisats
func (z *Zapper) GetBalance(ctx context.Context) (int64, error) {
	return z.nwcClient.GetBalance(ctx)
}