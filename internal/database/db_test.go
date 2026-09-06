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
		{"id": 1, "name": "Wingspan", "url": "wingspan.pdf", "image": "wingspan.webp", "min_players": 1, "max_players": 5, "best_players": "3", "complexity": 2.45},
		{"id": 2, "name": "Catan", "url": "catan.pdf", "image": "catan.webp", "min_players": 3, "max_players": 4, "best_players": "4", "complexity": 2.3}
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
	if games[0].BestPlayers != "3" && games[1].BestPlayers != "3" {
		t.Errorf("expected best_players 3 in seeded games, got %+v", games)
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
}

func TestDocumentsAndExpansions(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "docs_test.db")
	db, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// 1. Create Base Game
	baseGame := &Game{
		Name:        "Dune: Imperium",
		URL:         "dune-rules.pdf",
		Image:       "dune.jpg",
		MinPlayers:  1,
		MaxPlayers:  4,
		BestPlayers: "3-4",
		Complexity:  3.0,
	}
	if err := db.CreateGame(baseGame); err != nil {
		t.Fatalf("failed to create base game: %v", err)
	}

	// Verify default primary doc was created by CreateGame
	docs, err := db.ListDocuments(baseGame.ID)
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}
	if len(docs) != 1 || docs[0].Filename != "dune-rules.pdf" || !docs[0].IsPrimary {
		t.Fatalf("expected 1 primary doc, got %+v", docs)
	}

	// 2. Add Glossary Document
	glossary := &GameDocument{
		GameID:    baseGame.ID,
		Title:     "Reference Guide",
		Category:  "glossary",
		Filename:  "dune-reference.pdf",
		IsPrimary: false,
	}
	if err := db.AddDocument(glossary); err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	docs, err = db.ListDocuments(baseGame.ID)
	if err != nil || len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d (err: %v)", len(docs), err)
	}

	// 3. Add Child Expansion
	parentID := baseGame.ID
	expansion := &Game{
		ParentID:   &parentID,
		Name:       "Dune: Imperium - Rise of Ix",
		URL:        "rise-of-ix.pdf",
		Image:      "ix.jpg",
		MinPlayers: 1,
		MaxPlayers: 4,
		Complexity: 3.2,
	}
	if err := db.CreateGame(expansion); err != nil {
		t.Fatalf("failed to create expansion: %v", err)
	}

	// Verify GetGameWithDetails
	game, gameDocs, expansions, err := db.GetGameWithDetails(baseGame.ID)
	if err != nil {
		t.Fatalf("GetGameWithDetails failed: %v", err)
	}
	if game.Name != "Dune: Imperium" {
		t.Errorf("unexpected game name: %s", game.Name)
	}
	if len(gameDocs) != 2 {
		t.Errorf("expected 2 docs, got %d", len(gameDocs))
	}
	if len(expansions) != 1 || expansions[0].Name != "Dune: Imperium - Rise of Ix" {
		t.Errorf("expected 1 expansion, got %+v", expansions)
	}
}

func TestMigrationFromOldSchema(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "old_schema.db")
	
	// Create an old SQLite database without parent_id
	rawDB, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer rawDB.Close()

	// Verify parent_id column and index exist
	var count int
	err = rawDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('games') WHERE name = 'parent_id'").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected parent_id column in games table, got count %d, err %v", count, err)
	}
}

func TestListBaseGames(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "base_games_test.db")
	db, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	base := &Game{Name: "Dune: Imperium", URL: "dune.pdf", Image: "dune.jpg", MinPlayers: 2, MaxPlayers: 4}
	if err := db.CreateGame(base); err != nil {
		t.Fatalf("failed to create base: %v", err)
	}
	pid := base.ID
	exp := &Game{ParentID: &pid, Name: "Dune: Imperium - Rise of Ix", URL: "ix.pdf", Image: "ix.jpg", MinPlayers: 2, MaxPlayers: 4}
	if err := db.CreateGame(exp); err != nil {
		t.Fatalf("failed to create expansion: %v", err)
	}
	standalone := &Game{Name: "Wingspan", URL: "wingspan.pdf", Image: "wingspan.jpg", MinPlayers: 1, MaxPlayers: 5}
	if err := db.CreateGame(standalone); err != nil {
		t.Fatalf("failed to create standalone: %v", err)
	}

	bases, err := db.ListBaseGames("", 0)
	if err != nil {
		t.Fatalf("ListBaseGames failed: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("expected 2 base games (expansion excluded), got %d", len(bases))
	}

	// Search by expansion name must surface the parent
	byExp, err := db.ListBaseGames("Rise of Ix", 0)
	if err != nil {
		t.Fatalf("ListBaseGames search failed: %v", err)
	}
	if len(byExp) != 1 || byExp[0].Name != "Dune: Imperium" {
		t.Fatalf("expected parent surfaced by expansion search, got %+v", byExp)
	}

	// Player filter applies to base games only
	byPlayers, err := db.ListBaseGames("", 5)
	if err != nil {
		t.Fatalf("ListBaseGames player filter failed: %v", err)
	}
	if len(byPlayers) != 1 || byPlayers[0].Name != "Wingspan" {
		t.Fatalf("expected only Wingspan for 5 players, got %+v", byPlayers)
	}
}

func TestPDFOptimizationOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "pdf_test.db")
	db, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Initial check should be nil
	opt, err := db.GetPDFOptimization("rules.pdf")
	if err != nil {
		t.Fatalf("GetPDFOptimization failed: %v", err)
	}
	if opt != nil {
		t.Fatalf("expected nil for non-existent optimization, got %+v", opt)
	}

	// Save optimization
	newOpt := &PDFOptimization{
		Filename:     "rules.pdf",
		FileSize:     10240,
		ModTime:      1700000000,
		IsLinearized: true,
	}
	if err := db.SavePDFOptimization(newOpt); err != nil {
		t.Fatalf("SavePDFOptimization failed: %v", err)
	}

	// Fetch back
	opt, err = db.GetPDFOptimization("rules.pdf")
	if err != nil || opt == nil {
		t.Fatalf("GetPDFOptimization returned error or nil: %v", err)
	}
	if opt.Filename != "rules.pdf" || opt.FileSize != 10240 || !opt.IsLinearized {
		t.Fatalf("unexpected fetched optimization: %+v", opt)
	}

	// Update existing record
	newOpt.FileSize = 10500
	newOpt.IsLinearized = false
	if err := db.SavePDFOptimization(newOpt); err != nil {
		t.Fatalf("SavePDFOptimization update failed: %v", err)
	}

	opt, err = db.GetPDFOptimization("rules.pdf")
	if err != nil || opt == nil {
		t.Fatalf("GetPDFOptimization after update failed: %v", err)
	}
	if opt.FileSize != 10500 || opt.IsLinearized {
		t.Fatalf("expected updated size and false linearization: %+v", opt)
	}

	// Stats
	total, lin, pending, err := db.GetOptimizationStats()
	if err != nil {
		t.Fatalf("GetOptimizationStats failed: %v", err)
	}
	if total != 1 || lin != 0 || pending != 1 {
		t.Fatalf("expected total=1, lin=0, pending=1, got total=%d, lin=%d, pending=%d", total, lin, pending)
	}
}
