package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMigration0032RepairsLegacyWorkCardCDCTimestamp(t *testing.T) {
	dataDir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "ao.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// Version 0031's Workboard triggers stored work-card update times as Unix
	// milliseconds in change_log.created_at. Seed that historical shape before
	// applying the forward-only repair.
	upTo(t, db, 31)
	if _, err := db.Exec(`INSERT INTO projects (id, path, registered_at) VALUES ('workboard-cdc', '/tmp/workboard-cdc', datetime('now'))`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	legacyCreatedAt := time.Date(2026, time.July, 17, 3, 45, 6, 789_000_000, time.UTC)
	if _, err := db.Exec(
		`INSERT INTO change_log (project_id, event_type, payload, created_at) VALUES ('workboard-cdc', 'work_card_changed', '{"card_id":"legacy-card"}', ?)`,
		legacyCreatedAt.UnixMilli(),
	); err != nil {
		t.Fatalf("seed legacy CDC event: %v", err)
	}

	upTo(t, db, 32)

	var storageClass string
	if err := db.QueryRow(`SELECT typeof(created_at) FROM change_log WHERE event_type = 'work_card_changed'`).Scan(&storageClass); err != nil {
		t.Fatalf("read normalized storage class: %v", err)
	}
	if storageClass != "text" {
		t.Fatalf("created_at storage class = %q, want text", storageClass)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}

	s, err := Open(dataDir)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	events, err := s.EventsAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("read repaired CDC event: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if !events[0].CreatedAt.Equal(legacyCreatedAt.Truncate(time.Second)) {
		t.Fatalf("event created_at = %s, want %s", events[0].CreatedAt, legacyCreatedAt.Truncate(time.Second))
	}
}
