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

	"github.com/mistic0xb/pekka/internal/bunker"
	"github.com/mistic0xb/pekka/internal/logger"
	"github.com/mistic0xb/pekka/internal/nwc"
	"github.com/nbd-wtf/go-nostr"
)

type Zapper struct {
	nwcClient *nwc.Client
	pool      *nostr.SimplePool
	relays    []string
}

// New creates a new Zapper
func New(nwcURL string, relays []string, pool *nostr.SimplePool) (*Zapper, error) {
	logger.Log.Info().
		Str("component", "zapper").
		Msg("initializing zapper")

	client, err := nwc.NewClient(nwcURL)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to create NWC client")
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
	logger.Log.Info().Msg("connecting to NWC wallet")

	if err := z.nwcClient.Connect(ctx); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to connect to NWC wallet")
		return err
	}

	return nil
}

// Close closes NWC connection
func (z *Zapper) Close() {
	logger.Log.Info().Msg("closing NWC connection")
	z.nwcClient.Close()
}

// ZapNote sends a zap to a note
func (z *Zapper) ZapNote(
	ctx context.Context,
	eventID,
	authorPubkey string,
	amountSats int,
	comment string,
	bunkerClient *bunker.ReconnectingClient,
) error {

	logger.Log.Info().
		Str("event_id", eventID).
		Int("amount_sats", amountSats).
		Msg("starting zap")

	lightningAddress, err := z.getLightningAddress(ctx, authorPubkey)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("author_pubkey", authorPubkey).
			Msg("failed to get lightning address")
		return fmt.Errorf("failed to get lightning address: %w", err)
	}

	zapRequest, err := z.createZapRequest(ctx, eventID, authorPubkey, amountSats, comment, bunkerClient)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to create zap request")
		return fmt.Errorf("failed to create zap request: %w", err)
	}

	lnurlEndpoint := z.lightningAddressToLNURL(lightningAddress)

	invoice, err := z.requestInvoice(ctx, lnurlEndpoint, amountSats, zapRequest)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("lnurl", lnurlEndpoint).
			Msg("failed to request invoice")
		return err
	}

	if err := z.nwcClient.PayInvoice(ctx, invoice); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to pay invoice")
		return err
	}

	logger.Log.Info().
		Str("event_id", eventID).
		Msg("zap successful")

	return nil
}

// createZapRequest creates a kind 9734 zap request event
func (z *Zapper) createZapRequest(
	ctx context.Context,
	eventID,
	recipientPubkey string,
	amountSats int,
	comment string,
	bunkerClient *bunker.ReconnectingClient,
) (string, error) {

	zapperPubkey, err := bunkerClient.GetPublicKey(ctx)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to get zapper pubkey")
		return "", err
	}

	event := nostr.Event{
		PubKey:    zapperPubkey,
		CreatedAt: nostr.Now(),
		Kind:      9734,
		Tags: nostr.Tags{
			{"e", eventID},
			{"p", recipientPubkey},
			{"amount", fmt.Sprintf("%d", amountSats*1000)},
			{"relays", z.relays[0]},
		},
		Content: comment,
	}

	event.ID = event.GetID()

	if err := bunkerClient.SignEvent(ctx, &event); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to sign zap request")
		return "", fmt.Errorf("failed to sign zap request: %w", err)
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to marshal zap request")
		return "", fmt.Errorf("failed to marshal zap request: %w", err)
	}

	return string(eventJSON), nil
}

// getLightningAddress fetches the author's lightning address from profile (kind 0)
func (z *Zapper) getLightningAddress(ctx context.Context, pubkey string) (string, error) {
	logger.Log.Debug().
		Str("pubkey", pubkey).
		Msg("fetching lightning address")

	filters := []nostr.Filter{{
		Kinds:   []int{0},
		Authors: []string{pubkey},
		Limit:   1,
	}}

	profileCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for event := range z.pool.FetchMany(profileCtx, z.relays, filters[0]) {
		var profile struct {
			LUD16 string `json:"lud16"`
		}

		if err := json.Unmarshal([]byte(event.Content), &profile); err != nil {
			logger.Log.Debug().
				Err(err).
				Msg("failed to parse profile metadata")
			continue
		}

		if profile.LUD16 != "" {
			return profile.LUD16, nil
		}
	}

	return "", fmt.Errorf("no lightning address found in profile")
}

// lightningAddressToLNURL converts address to LNURL endpoint
func (z *Zapper) lightningAddressToLNURL(address string) string {
	parts := strings.Split(address, "@")
	if len(parts) != 2 {
		return ""
	}
	return fmt.Sprintf("https://%s/.well-known/lnurlp/%s", parts[1], parts[0])
}

// requestInvoice requests a lightning invoice
func (z *Zapper) requestInvoice(ctx context.Context, lnurlEndpoint string, amountSats int, zapRequest string) (string, error) {
	metadata, err := z.fetchLNURLMetadata(lnurlEndpoint)
	if err != nil {
		return "", err
	}

	amountMillisats := int64(amountSats * 1000)

	if amountMillisats < metadata.MinSendable || amountMillisats > metadata.MaxSendable {
		err := fmt.Errorf("amount %d out of bounds (%d-%d)", amountMillisats, metadata.MinSendable, metadata.MaxSendable)
		logger.Log.Error().Err(err).Msg("invalid zap amount")
		return "", err
	}

	return z.fetchInvoice(metadata.Callback, amountMillisats, zapRequest)
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

// fetchLNURLMetadata fetches LNURL metadata
func (z *Zapper) fetchLNURLMetadata(endpoint string) (*LNURLPayMetadata, error) {
	logger.Log.Debug().
		Str("endpoint", endpoint).
		Msg("fetching LNURL metadata")

	resp, err := http.Get(endpoint)
	if err != nil {
		logger.Log.Error().Err(err).Msg("LNURL request failed")
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err := fmt.Errorf("LNURL returned status %d", resp.StatusCode)
		logger.Log.Error().Err(err).Msg("invalid LNURL response")
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to read LNURL response")
		return nil, err
	}

	var metadata LNURLPayMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		logger.Log.Error().Err(err).Msg("failed to parse LNURL metadata")
		return nil, err
	}

	if metadata.Tag != "payRequest" {
		err := fmt.Errorf("invalid tag %s", metadata.Tag)
		logger.Log.Error().Err(err).Msg("invalid LNURL tag")
		return nil, err
	}

	return &metadata, nil
}

// fetchInvoice requests an invoice from callback
func (z *Zapper) fetchInvoice(callback string, amountMillisats int64, zapRequest string) (string, error) {
	callbackURL, err := url.Parse(callback)
	if err != nil {
		logger.Log.Error().Err(err).Msg("invalid callback URL")
		return "", err
	}

	q := callbackURL.Query()
	q.Set("amount", strconv.FormatInt(amountMillisats, 10))
	q.Set("nostr", zapRequest)
	callbackURL.RawQuery = q.Encode()

	resp, err := http.Get(callbackURL.String())
	if err != nil {
		logger.Log.Error().Err(err).Msg("invoice request failed")
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err := fmt.Errorf("callback returned status %d", resp.StatusCode)
		logger.Log.Error().Err(err).Msg("invoice callback error")
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to read invoice response")
		return "", err
	}

	var invoiceResponse struct {
		PR     string `json:"pr"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}

	if err := json.Unmarshal(body, &invoiceResponse); err != nil {
		logger.Log.Error().Err(err).Msg("failed to parse invoice response")
		return "", err
	}

	if invoiceResponse.Status == "ERROR" {
		err := fmt.Errorf("LNURL error: %s", invoiceResponse.Reason)
		logger.Log.Error().Err(err).Msg("LNURL returned error")
		return "", err
	}

	if invoiceResponse.PR == "" {
		err := fmt.Errorf("no invoice in response")
		logger.Log.Error().Err(err).Msg("empty invoice")
		return "", err
	}

	return invoiceResponse.PR, nil
}

// GetBalance gets wallet balance
func (z *Zapper) GetBalance(ctx context.Context) (int64, error) {
	return z.nwcClient.GetBalance(ctx)
}
