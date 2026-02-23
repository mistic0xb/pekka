package nostrlist

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mistic0xb/pekka/internal/bunker"
	"github.com/mistic0xb/pekka/internal/logger"

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
	bunkerClient *bunker.ReconnectingClient,
	pool *nostr.SimplePool,
) ([]*PrivateList, error) {

	logger.Log.Info().
		Str("author_npub", authorNPub).
		Int("relay_count", len(relayURLs)).
		Strs("relays", relayURLs).
		Msg("starting private list fetch")

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
			Str("expected", "npub").
			Msg("unexpected nip19 prefix")
		return nil, fmt.Errorf("expected npub prefix, got %s", prefix)
	}

	pubkeyHexStr := pubkeyHex.(string)
	logger.Log.Info().
		Str("pubkey_hex", pubkeyHexStr).
		Msg("decoded npub to hex pubkey")

	filter := nostr.Filter{
		Kinds:   []int{30000},
		Authors: []string{pubkeyHexStr},
	}

	logger.Log.Info().
		Int("kind", 30000).
		Str("author", pubkeyHexStr).
		Msg("created filter for kind 30000 (NIP-51 private lists)")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	events := make([]nostr.RelayEvent, 0)
	relayStats := make(map[string]int)

	logger.Log.Info().Msg("connecting to relays and fetching events")
	fetchStart := time.Now()

	for ev := range pool.FetchMany(ctx, relayURLs, filter) {
		relayStats[ev.Relay.URL]++
		
		logger.Log.Debug().
			Str("relay", ev.Relay.URL).
			Str("event_id", ev.ID).
			Time("created_at", time.Unix(int64(ev.CreatedAt), 0)).
			Int("tag_count", len(ev.Tags)).
			Int("content_length", len(ev.Content)).
			Bool("has_encrypted_content", ev.Content != "").
			Str("pubkey", ev.PubKey).
			Msg("received event from relay")

		events = append(events, ev)
	}

	fetchDuration := time.Since(fetchStart).Milliseconds()

	// Log relay statistics
	logger.Log.Info().
		Dur("duration", time.Duration(fetchDuration)).
		Msg("completed relay fetch")

	for relayURL, count := range relayStats {
		logger.Log.Info().
			Str("relay", relayURL).
			Int("event_count", count).
			Msg("relay response summary")
	}

	// Check for relays that didn't respond
	silentRelays := 0
	for _, relayURL := range relayURLs {
		if _, found := relayStats[relayURL]; !found {
			silentRelays++
			logger.Log.Warn().
				Str("relay", relayURL).
				Msg("relay did not return any events (may be offline, no data, or slow)")
		}
	}

	logger.Log.Info().
		Int("total_events", len(events)).
		Int("responding_relays", len(relayStats)).
		Int("silent_relays", silentRelays).
		Int("total_relays", len(relayURLs)).
		Msg("relay fetch summary")

	if len(events) == 0 {
		logger.Log.Warn().
			Int("relay_count", len(relayURLs)).
			Msg("no events received from any relay - check if lists exist or relays are reachable")
		return []*PrivateList{}, nil
	}

	return processEvents(events, bunkerClient, pubkeyHexStr)
}

// processEvents converts raw events into PrivateList structs
func processEvents(
	events []nostr.RelayEvent,
	bunkerClient *bunker.ReconnectingClient,
	pubkeyHex string,
) ([]*PrivateList, error) {

	logger.Log.Info().
		Int("event_count", len(events)).
		Msg("processing events into private lists")

	lists := make([]*PrivateList, 0)
	seen := make(map[string]*nostr.RelayEvent) // Track newest event per list ID
	skippedEvents := 0

	for i, event := range events {
		logger.Log.Debug().
			Int("event_index", i).
			Str("event_id", event.ID).
			Str("relay", event.Relay.URL).
			Msg("processing event")

		var listID string

		// Find the 'd' tag (list identifier)
		for _, tag := range event.Tags {
			if len(tag) >= 2 && tag[0] == "d" {
				listID = tag[1]
				logger.Log.Debug().
					Str("list_id", listID).
					Str("event_id", event.ID).
					Msg("found list ID in 'd' tag")
				break
			}
		}

		if listID == "" {
			logger.Log.Warn().
				Str("event_id", event.ID).
				Str("relay", event.Relay.URL).
				Msg("skipping event without 'd' tag (no list ID)")
			skippedEvents++
			continue
		}

		// For replaceable events (kind 30000), keep only the newest
		if existing, exists := seen[listID]; exists {
			if event.CreatedAt > existing.CreatedAt {
				logger.Log.Debug().
					Str("list_id", listID).
					Str("old_event_id", existing.ID).
					Time("old_created_at", time.Unix(int64(existing.CreatedAt), 0)).
					Str("new_event_id", event.ID).
					Time("new_created_at", time.Unix(int64(event.CreatedAt), 0)).
					Msg("replacing with newer event")
				seen[listID] = &event
			} else {
				logger.Log.Debug().
					Str("list_id", listID).
					Str("event_id", event.ID).
					Msg("skipping older duplicate event")
			}
			continue
		}

		seen[listID] = &event
	}

	logger.Log.Info().
		Int("unique_lists", len(seen)).
		Int("duplicate_events", len(events)-len(seen)-skippedEvents).
		Int("skipped_events", skippedEvents).
		Msg("deduplicated events")

	// Process each unique list
	for listID, event := range seen {
		logger.Log.Debug().
			Str("list_id", listID).
			Str("event_id", event.ID).
			Msg("extracting list metadata and members")

		// Find title
		title := listID // Default to list ID
		for _, tag := range event.Tags {
			if len(tag) >= 2 && (tag[0] == "name" || tag[0] == "title") && tag[1] != "" {
				title = tag[1]
				logger.Log.Debug().
					Str("list_id", listID).
					Str("title", title).
					Str("tag_type", tag[0]).
					Msg("found list title")
				break
			}
		}

		// Extract npubs
		npubs, hasPrivate := extractAllNPubs(*event, bunkerClient, pubkeyHex)

		logger.Log.Info().
			Str("list_id", listID).
			Str("title", title).
			Int("member_count", len(npubs)).
			Bool("has_private_members", hasPrivate).
			Str("event_id", event.ID).
			Msg("processed list")

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
		Msg("completed processing private lists")

	return lists, nil
}

// extractAllNPubs extracts npubs from public tags and encrypted content
func extractAllNPubs(
	event nostr.RelayEvent,
	bunkerClient *bunker.ReconnectingClient,
	pubkeyHex string,
) ([]string, bool) {

	npubSet := make(map[string]bool)
	hasPrivate := false
	publicCount := 0

	logger.Log.Debug().
		Str("event_id", event.ID).
		Msg("extracting npubs from event")

	// Public tags
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			if npub, err := nip19.EncodePublicKey(tag[1]); err == nil {
				npubSet[npub] = true
				publicCount++
				logger.Log.Debug().
					Str("npub", npub).
					Str("hex", tag[1]).
					Msg("found public member in 'p' tag")
			} else {
				logger.Log.Warn().
					Err(err).
					Str("hex", tag[1]).
					Msg("failed to encode public key to npub")
			}
		}
	}

	logger.Log.Debug().
		Str("event_id", event.ID).
		Int("public_members", publicCount).
		Msg("extracted public members")

	// Encrypted content - for NIP-51 lists, this is self-encrypted
	// The content is encrypted by you to yourself, so we pass your own pubkey
	if event.Content != "" {
		logger.Log.Debug().
			Str("event_id", event.ID).
			Int("content_length", len(event.Content)).
			Str("author_pubkey", event.PubKey).
			Msg("attempting to decrypt private content (self-encrypted)")

		plaintext, err := decryptContent(event.Content, bunkerClient, event.PubKey)
		if err != nil {
			logger.Log.Error().
				Err(err).
				Str("event_id", event.ID).
				Str("author_pubkey", event.PubKey).
				Msg("failed to decrypt private list content")
		} else if plaintext != "" {
			logger.Log.Debug().
				Str("event_id", event.ID).
				Int("plaintext_length", len(plaintext)).
				Msg("decryption successful, parsing tags")

			privateTags := parseDecryptedTags(plaintext)
			privateCount := 0

			for _, tag := range privateTags {
				if len(tag) >= 2 && tag[0] == "p" {
					if npub, err := nip19.EncodePublicKey(tag[1]); err == nil {
						npubSet[npub] = true
						hasPrivate = true
						privateCount++
						logger.Log.Debug().
							Str("npub", npub).
							Str("hex", tag[1]).
							Msg("found private member")
					} else {
						logger.Log.Warn().
							Err(err).
							Str("hex", tag[1]).
							Msg("failed to encode private member public key")
					}
				}
			}

			logger.Log.Info().
				Str("event_id", event.ID).
				Int("private_members", privateCount).
				Int("total_private_tags", len(privateTags)).
				Msg("extracted private members")
		}
	} else {
		logger.Log.Debug().
			Str("event_id", event.ID).
			Msg("no encrypted content in event")
	}

	npubs := npubsFromSet(npubSet)

	logger.Log.Debug().
		Str("event_id", event.ID).
		Int("total_unique_members", len(npubs)).
		Int("public", publicCount).
		Bool("has_private", hasPrivate).
		Msg("completed npub extraction")

	return npubs, hasPrivate
}

// decryptContent tries NIP-44 first, then NIP-04
func decryptContent(
	content string,
	bunkerClient *bunker.ReconnectingClient,
	pubkeyHex string,
) (string, error) {

	logger.Log.Debug().
		Int("ciphertext_length", len(content)).
		Msg("attempting decryption")

	// Try NIP-44 first - fresh context
	logger.Log.Debug().Msg("trying NIP-44 decryption")
	ctx44, cancel44 := context.WithTimeout(context.Background(), 30*time.Second)
	plaintext, err := bunkerClient.DecryptNIP44(ctx44, pubkeyHex, content)
	cancel44()
	
	if err == nil {
		logger.Log.Info().
			Int("plaintext_length", len(plaintext)).
			Msg("NIP-44 decryption succeeded")
		return plaintext, nil
	}

	logger.Log.Debug().
		Err(err).
		Msg("NIP-44 decryption failed, falling back to NIP-04")

	// Fallback to NIP-04 - fresh context
	ctx04, cancel04 := context.WithTimeout(context.Background(), 30*time.Second)
	plaintext, err = bunkerClient.DecryptNIP04(ctx04, pubkeyHex, content)
	cancel04()
	
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("NIP-04 decryption also failed - both methods exhausted")
		return "", fmt.Errorf("decryption failed (tried NIP-44 and NIP-04): %w", err)
	}

	logger.Log.Info().
		Int("plaintext_length", len(plaintext)).
		Msg("NIP-04 decryption succeeded")

	return plaintext, nil
}

// parseDecryptedTags parses decrypted JSON tags
func parseDecryptedTags(content string) [][]string {
	logger.Log.Debug().
		Int("content_length", len(content)).
		Msg("parsing decrypted tags JSON")

	var tags [][]string
	if err := json.Unmarshal([]byte(content), &tags); err != nil {
		logger.Log.Error().
			Err(err).
			Str("content_preview", truncate(content, 100)).
			Msg("failed to parse decrypted tags JSON")
		return nil
	}

	logger.Log.Debug().
		Int("tag_count", len(tags)).
		Msg("successfully parsed tags")

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
	bunkerClient *bunker.ReconnectingClient,
	pool *nostr.SimplePool,
	listID string,
) ([]string, error) {

	logger.Log.Info().
		Str("list_id", listID).
		Str("author_npub", authorNPub).
		Msg("fetching npubs from specific list")

	lists, err := FetchPrivateLists(relays, authorNPub, bunkerClient, pool)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("list_id", listID).
			Msg("failed to fetch private lists")
		return nil, err
	}

	logger.Log.Debug().
		Int("total_lists", len(lists)).
		Str("target_list_id", listID).
		Msg("searching for target list")

	for _, list := range lists {
		if list.ID == listID {
			logger.Log.Info().
				Str("list_id", listID).
				Str("title", list.Title).
				Int("member_count", len(list.NPubs)).
				Msg("found target list")
			return list.NPubs, nil
		}
	}

	// List available IDs for debugging
	availableIDs := make([]string, len(lists))
	for i, list := range lists {
		availableIDs[i] = list.ID
	}

	logger.Log.Error().
		Str("list_id", listID).
		Strs("available_list_ids", availableIDs).
		Msg("list not found")

	return nil, fmt.Errorf("list '%s' not found", listID)
}

// truncate helper for safe logging of potentially long strings
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}