package nostrlist

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mistic0xb/zapbot/internal/bunker"
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

// FetchPrivateLists now takes bunker client instead of nsec
func FetchPrivateLists(relayURLs []string, authorNPub string, bunkerClient *bunker.Client, pool *nostr.SimplePool) ([]*PrivateList, error) {
	// Decode npub to hex
	prefix, pubkeyHex, err := nip19.Decode(authorNPub)
	if err != nil {
		return nil, fmt.Errorf("invalid npub: %w", err)
	}
	if prefix != "npub" {
		return nil, fmt.Errorf("expected npub prefix")
	}

	// Create filter for kind 30000
	filter := nostr.Filter{
		Kinds:   []int{30000},
		Authors: []string{pubkeyHex.(string)},
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	// Collect events from relays
	eventChan := make(chan nostr.RelayEvent, 100)
	// pool := nostr.NewSimplePool(ctx)

	for event := range pool.FetchMany(ctx, relayURLs, filter) {
		eventChan <- event
	}
	close(eventChan)

	// Collect all events
	events := make([]nostr.RelayEvent, 0)
	for event := range eventChan {
		events = append(events, event)
	}

	// Process events into lists
	return processEvents(events, bunkerClient, pubkeyHex.(string))
}

// processEvents converts raw events into PrivateList structs
func processEvents(events []nostr.RelayEvent, bunkerClient *bunker.Client, pubkeyHex string) ([]*PrivateList, error) {
	lists := make([]*PrivateList, 0)
	seen := make(map[string]bool)

	for _, event := range events {
		// Extract "d" tag (list identifier)
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

		// Skip duplicates
		if seen[listID] {
			continue
		}
		seen[listID] = true

		// Extract name
		title := listID
		for _, tag := range event.Tags {
			if len(tag) >= 2 && tag[0] == "name" && tag[1] != "" {
				title = tag[1]
				break
			}
		}
		if title == listID {
			for _, tag := range event.Tags {
				if len(tag) >= 2 && tag[0] == "title" && tag[1] != "" {
					title = tag[1]
					break
				}
			}
		}

		// Get npubs from both tags and encrypted content
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

	return lists, nil
}

// extractAllNPubs gets npubs from both public tags AND encrypted content
func extractAllNPubs(event nostr.RelayEvent, bunkerClient *bunker.Client, pubkeyHex string) ([]string, bool) {
	npubSet := make(map[string]bool)
	hasPrivate := false

	// 1. Extract from public tags
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			npub, err := nip19.EncodePublicKey(tag[1])
			if err == nil {
				npubSet[npub] = true
			}
		}
	}

	// 2. Decrypt content and extract private members
	if event.Content != "" {
		fmt.Printf("Decrypting private content...\n")

		plaintext, err := decryptContent(event.Content, bunkerClient, pubkeyHex)
		if err != nil {
			fmt.Printf("Decryption failed: %v\n", err)
		} else if plaintext != "" {
			fmt.Printf("Decrypted successfully\n\n")
			privateTags := parseDecryptedTags(plaintext)
			for _, tag := range privateTags {
				if len(tag) >= 2 && tag[0] == "p" {
					npub, err := nip19.EncodePublicKey(tag[1])
					if err == nil {
						npubSet[npub] = true
						hasPrivate = true
					}
				}
			}
		}
	}

	return npubsFromSet(npubSet), hasPrivate
}

// decryptContent tries NIP-44 first, then falls back to NIP-04
func decryptContent(content string, bunkerClient *bunker.Client, pubkeyHex string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try NIP-44 (modern encryption)
	plaintext, err := bunkerClient.DecryptNIP44(ctx, pubkeyHex, content)
	if err == nil {
		fmt.Printf("NIP-44 decryption worked\n")
		return plaintext, nil
	}
	fmt.Printf("NIP-44 decryption failed, trying NIP-04: %v\n", err)

	// Fallback to NIP-04 (legacy encryption)
	plaintext, err = bunkerClient.DecryptNIP04(ctx, pubkeyHex, content)
	if err != nil {
		fmt.Printf("NIP-04 failed: %v\n", err)
		return "", fmt.Errorf("NIP-04 decryption failed: %w", err)
	}
	return plaintext, nil
}

// parseDecryptedTags parses the decrypted content as tag array
func parseDecryptedTags(content string) [][]string {
	var tags [][]string
	if err := json.Unmarshal([]byte(content), &tags); err != nil {
		return nil
	}
	return tags
}

// npubsFromSet converts a set to a slice
func npubsFromSet(npubSet map[string]bool) []string {
	npubs := make([]string, 0, len(npubSet))
	for npub := range npubSet {
		npubs = append(npubs, npub)
	}
	return npubs
}

// GetNPubsFromList fetches a specific list by ID
func GetNPubsFromList(relays []string, authorNPub string, bunkerClient *bunker.Client, pool *nostr.SimplePool, listID string) ([]string, error) {
	lists, err := FetchPrivateLists(relays, authorNPub, bunkerClient, pool)
	if err != nil {
		return nil, err
	}

	for _, list := range lists {
		if list.ID == listID {
			return list.NPubs, nil
		}
	}

	return nil, fmt.Errorf("list '%s' not found", listID)
}
