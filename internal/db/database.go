package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

// ZappedEvent represents a record of a zapped event
type ZappedEvent struct {
	EventID       string
	AuthorPubkey  string
	ZappedAt      int64
	Amount        int
	EventCreatedAt int64
}

// Open opens/creates the SQLite database
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{conn: conn}

	// Initialize schema
	if err := db.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// initSchema creates tables if they don't exist
func (db *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS zapped_events (
		event_id TEXT PRIMARY KEY,
		author_pubkey TEXT NOT NULL,
		zapped_at INTEGER NOT NULL,
		amount INTEGER NOT NULL,
		event_created_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_author ON zapped_events(author_pubkey);
	CREATE INDEX IF NOT EXISTS idx_zapped_at ON zapped_events(zapped_at);
	CREATE INDEX IF NOT EXISTS idx_event_created_at ON zapped_events(event_created_at);
	`

	_, err := db.conn.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// IsZapped checks if an event has already been zapped
func (db *DB) IsZapped(eventID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM zapped_events WHERE event_id = ?)`
	
	err := db.conn.QueryRow(query, eventID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if zapped: %w", err)
	}

	return exists, nil
}

// MarkZapped records that an event has been zapped
func (db *DB) MarkZapped(eventID, authorPubkey string, amount int, eventCreatedAt int64) error {
	query := `
		INSERT INTO zapped_events (event_id, author_pubkey, zapped_at, amount, event_created_at)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := db.conn.Exec(query, eventID, authorPubkey, time.Now().Unix(), amount, eventCreatedAt)
	if err != nil {
		return fmt.Errorf("failed to mark as zapped: %w", err)
	}

	return nil
}

// GetTodayTotal returns total sats zapped today
func (db *DB) GetTodayTotal() (int, error) {
	// Start of today (midnight UTC)
	today := time.Now().UTC().Truncate(24 * time.Hour).Unix()

	var total sql.NullInt64
	query := `SELECT SUM(amount) FROM zapped_events WHERE zapped_at >= ?`

	err := db.conn.QueryRow(query, today).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get today's total: %w", err)
	}

	if !total.Valid {
		return 0, nil
	}

	return int(total.Int64), nil
}

// GetTodayTotalForAuthor returns total sats zapped to a specific author today
func (db *DB) GetTodayTotalForAuthor(pubkey string) (int, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour).Unix()

	var total sql.NullInt64
	query := `SELECT SUM(amount) FROM zapped_events WHERE author_pubkey = ? AND zapped_at >= ?`

	err := db.conn.QueryRow(query, pubkey, today).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get author's today total: %w", err)
	}

	if !total.Valid {
		return 0, nil
	}

	return int(total.Int64), nil
}

// GetStats returns overall statistics
func (db *DB) GetStats() (*Stats, error) {
	stats := &Stats{}

	// Total events zapped
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM zapped_events`).Scan(&stats.TotalZapped)
	if err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	// Total sats spent (all time)
	var totalSats sql.NullInt64
	err = db.conn.QueryRow(`SELECT SUM(amount) FROM zapped_events`).Scan(&totalSats)
	if err != nil {
		return nil, fmt.Errorf("failed to get total sats: %w", err)
	}
	if totalSats.Valid {
		stats.TotalSats = int(totalSats.Int64)
	}

	// Today's total
	stats.TodayTotal, err = db.GetTodayTotal()
	if err != nil {
		return nil, err
	}

	// Count of unique authors zapped
	err = db.conn.QueryRow(`SELECT COUNT(DISTINCT author_pubkey) FROM zapped_events`).Scan(&stats.UniqueAuthors)
	if err != nil {
		return nil, fmt.Errorf("failed to get unique authors: %w", err)
	}

	return stats, nil
}

// GetRecentZaps returns the N most recent zaps
func (db *DB) GetRecentZaps(limit int) ([]ZappedEvent, error) {
	query := `
		SELECT event_id, author_pubkey, zapped_at, amount, event_created_at
		FROM zapped_events
		ORDER BY zapped_at DESC
		LIMIT ?
	`

	rows, err := db.conn.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent zaps: %w", err)
	}
	defer rows.Close()

	var zaps []ZappedEvent
	for rows.Next() {
		var z ZappedEvent
		err := rows.Scan(&z.EventID, &z.AuthorPubkey, &z.ZappedAt, &z.Amount, &z.EventCreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		zaps = append(zaps, z)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return zaps, nil
}

// Stats holds database statistics
type Stats struct {
	TotalZapped   int
	TotalSats     int
	TodayTotal    int
	UniqueAuthors int
}