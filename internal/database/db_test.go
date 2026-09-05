package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	seedPath := filepath.Join(tempDir, "seed.json")

	seedContent := `[
		{"id": 1, "name": "Wingspan", "url": "wingspan.pdf", "image": "wingspan.webp", "min_players": 1, "max_players": 5},
		{"id": 2, "name": "Catan", "url": "catan.pdf", "image": "catan.webp", "min_players": 3, "max_players": 4}
	]`
	if err := os.WriteFile(seedPath, []byte(seedContent), 0644); err != nil {
		t.Fatalf("failed to write seed file: %v", err)
	}

	db, err := Open(dbPath, seedPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Verify seeding
	games, err := db.ListGames("", 0)
	if err != nil {
		t.Fatalf("ListGames failed: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("expected 2 seeded games, got %d", len(games))
	}

	// Test Search
	searchRes, err := db.ListGames("Wing", 0)
	if err != nil || len(searchRes) != 1 || searchRes[0].Name != "Wingspan" {
		t.Fatalf("search for 'Wing' failed, got %v", searchRes)
	}

	// Test Player Filter
	soloRes, err := db.ListGames("", 1)
	if err != nil || len(soloRes) != 1 || soloRes[0].Name != "Wingspan" {
		t.Fatalf("player filter for 1 failed, got %v", soloRes)
	}

	fourPlayerRes, err := db.ListGames("", 4)
	if err != nil || len(fourPlayerRes) != 2 {
		t.Fatalf("player filter for 4 failed, expected 2 games, got %d", len(fourPlayerRes))
	}
}
