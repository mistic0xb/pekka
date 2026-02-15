package nostrlist

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mistic0xb/zapbot/internal/bunker"
	"github.com/mistic0xb/zapbot/internal/logger"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// PrivateList represents a NIP-51 private list
type PrivateList struct {
	ID         string
	Title      string
	NPubs      []string
	EventID    string
	CreatedAt  int64
	HasPrivate bool
}

// FetchPrivateLists fetches private lists for an author
func FetchPrivateLists(
	relayURLs []string,
	authorNPub string,
	bunkerClient *bunker.Client,
	pool *nostr.SimplePool,
) ([]*PrivateList, error) {

	logger.Log.Info().
		Str("author_npub", authorNPub).
		Int("relay_count", len(relayURLs)).
		Msg("fetching private lists")

	// Decode npub to hex
	prefix, pubkeyHex, err := nip19.Decode(authorNPub)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("npub", authorNPub).
			Msg("failed to decode npub")
		return nil, fmt.Errorf("invalid npub: %w", err)
	}

	if prefix != "npub" {
		logger.Log.Error().
			Str("prefix", prefix).
			Msg("unexpected nip19 prefix")
		return nil, fmt.Errorf("expected npub prefix")
	}

	filter := nostr.Filter{
		Kinds:   []int{30000},
		Authors: []string{pubkeyHex.(string)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	events := make([]nostr.RelayEvent, 0)

	for ev := range pool.FetchMany(ctx, relayURLs, filter) {
		events = append(events, ev)
	}

	logger.Log.Info().
		Int("event_count", len(events)).
		Msg("events fetched from relays")

	return processEvents(events, bunkerClient, pubkeyHex.(string))
}

// processEvents converts raw events into PrivateList structs
func processEvents(
	events []nostr.RelayEvent,
	bunkerClient *bunker.Client,
	pubkeyHex string,
) ([]*PrivateList, error) {

	lists := make([]*PrivateList, 0)
	seen := make(map[string]bool)

	for _, event := range events {
		var listID string

		for _, tag := range event.Tags {
			if len(tag) >= 2 && tag[0] == "d" {
				listID = tag[1]
				break
			}
		}
		if listID == "" {
			continue
		}

		if seen[listID] {
			continue
		}
		seen[listID] = true

		title := listID
		for _, tag := range event.Tags {
			if len(tag) >= 2 && (tag[0] == "name" || tag[0] == "title") && tag[1] != "" {
				title = tag[1]
				break
			}
		}

		npubs, hasPrivate := extractAllNPubs(event, bunkerClient, pubkeyHex)

		lists = append(lists, &PrivateList{
			ID:         listID,
			Title:      title,
			NPubs:      npubs,
			EventID:    event.ID,
			CreatedAt:  int64(event.CreatedAt),
			HasPrivate: hasPrivate,
		})
	}

	logger.Log.Info().
		Int("list_count", len(lists)).
		Msg("private lists processed")

	return lists, nil
}

// extractAllNPubs extracts npubs from public tags and encrypted content
func extractAllNPubs(
	event nostr.RelayEvent,
	bunkerClient *bunker.Client,
	pubkeyHex string,
) ([]string, bool) {

	npubSet := make(map[string]bool)
	hasPrivate := false

	// Public tags
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			if npub, err := nip19.EncodePublicKey(tag[1]); err == nil {
				npubSet[npub] = true
			}
		}
	}

	// Encrypted content
	if event.Content != "" {
		plaintext, err := decryptContent(event.Content, bunkerClient, pubkeyHex)
		if err != nil {
			logger.Log.Error().
				Err(err).
				Str("event_id", event.ID).
				Msg("failed to decrypt private list content")
		} else if plaintext != "" {
			privateTags := parseDecryptedTags(plaintext)
			for _, tag := range privateTags {
				if len(tag) >= 2 && tag[0] == "p" {
					if npub, err := nip19.EncodePublicKey(tag[1]); err == nil {
						npubSet[npub] = true
						hasPrivate = true
					}
				}
			}
		}
	}

	return npubsFromSet(npubSet), hasPrivate
}

// decryptContent tries NIP-44 first, then NIP-04
func decryptContent(
	content string,
	bunkerClient *bunker.Client,
	pubkeyHex string,
) (string, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	plaintext, err := bunkerClient.DecryptNIP44(ctx, pubkeyHex, content)
	if err == nil {
		logger.Log.Info().
			Msg("NIP-44 decryption succeeded")
		return plaintext, nil
	}

	logger.Log.Info().
		Err(err).
		Msg("NIP-44 decryption failed, falling back to NIP-04")

	plaintext, err = bunkerClient.DecryptNIP04(ctx, pubkeyHex, content)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("NIP-04 decryption failed")
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// parseDecryptedTags parses decrypted JSON tags
func parseDecryptedTags(content string) [][]string {
	var tags [][]string
	if err := json.Unmarshal([]byte(content), &tags); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to parse decrypted tags JSON")
		return nil
	}
	return tags
}

// npubsFromSet converts set to slice
func npubsFromSet(npubSet map[string]bool) []string {
	npubs := make([]string, 0, len(npubSet))
	for npub := range npubSet {
		npubs = append(npubs, npub)
	}
	return npubs
}

// GetNPubsFromList fetches a specific list by ID
func GetNPubsFromList(
	relays []string,
	authorNPub string,
	bunkerClient *bunker.Client,
	pool *nostr.SimplePool,
	listID string,
) ([]string, error) {

	logger.Log.Info().
		Str("list_id", listID).
		Msg("fetching npubs from list")

	lists, err := FetchPrivateLists(relays, authorNPub, bunkerClient, pool)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to fetch private lists")
		return nil, err
	}

	for _, list := range lists {
		if list.ID == listID {
			return list.NPubs, nil
		}
	}

	logger.Log.Error().
		Str("list_id", listID).
		Msg("list not found")

	return nil, fmt.Errorf("list '%s' not found", listID)
}
