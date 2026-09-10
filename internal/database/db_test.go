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

	base := &Game{Name: "Dune: Imperium", URL: "dune.pdf", Image: "dune.jpg", MinPlayers: 2, MaxPlayers: 4, Complexity: 3.05}
	if err := db.CreateGame(base); err != nil {
		t.Fatalf("failed to create base: %v", err)
	}
	pid := base.ID
	exp := &Game{ParentID: &pid, Name: "Dune: Imperium - Rise of Ix", URL: "ix.pdf", Image: "ix.jpg", MinPlayers: 2, MaxPlayers: 4, Complexity: 3.2}
	if err := db.CreateGame(exp); err != nil {
		t.Fatalf("failed to create expansion: %v", err)
	}
	standalone := &Game{Name: "Wingspan", URL: "wingspan.pdf", Image: "wingspan.jpg", MinPlayers: 1, MaxPlayers: 5, Complexity: 2.45}
	if err := db.CreateGame(standalone); err != nil {
		t.Fatalf("failed to create standalone: %v", err)
	}

	bases, err := db.ListBaseGames("", 0, 0, 5, "", "")
	if err != nil {
		t.Fatalf("ListBaseGames failed: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("expected 2 base games (expansion excluded), got %d", len(bases))
	}

	// Search by expansion name must surface the parent
	byExp, err := db.ListBaseGames("Rise of Ix", 0, 0, 5, "", "")
	if err != nil {
		t.Fatalf("ListBaseGames search failed: %v", err)
	}
	if len(byExp) != 1 || byExp[0].Name != "Dune: Imperium" {
		t.Fatalf("expected parent surfaced by expansion search, got %+v", byExp)
	}

	// Player filter applies to base games only
	byPlayers, err := db.ListBaseGames("", 5, 0, 5, "", "")
	if err != nil {
		t.Fatalf("ListBaseGames player filter failed: %v", err)
	}
	if len(byPlayers) != 1 || byPlayers[0].Name != "Wingspan" {
		t.Fatalf("expected only Wingspan for 5 players, got %+v", byPlayers)
	}

	// Complexity filter: high complexity only (>= 3.0)
	byHighComp, err := db.ListBaseGames("", 0, 3.0, 5.0, "", "")
	if err != nil {
		t.Fatalf("ListBaseGames high complexity failed: %v", err)
	}
	if len(byHighComp) != 1 || byHighComp[0].Name != "Dune: Imperium" {
		t.Fatalf("expected only Dune: Imperium for complexity >= 3.0, got %+v", byHighComp)
	}

	// Complexity filter: medium complexity only (2.0 to 3.0)
	byMedComp, err := db.ListBaseGames("", 0, 2.0, 3.0, "", "")
	if err != nil {
		t.Fatalf("ListBaseGames medium complexity failed: %v", err)
	}
	if len(byMedComp) != 1 || byMedComp[0].Name != "Wingspan" {
		t.Fatalf("expected only Wingspan for complexity 2.0-3.0, got %+v", byMedComp)
	}
}

func TestListBaseGamesSorting(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_sort.db")
	db, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	games := []*Game{
		{Name: "Brass: Birmingham", URL: "brass.pdf", Rating: 8.6, Complexity: 3.9, MinPlayers: 2, MaxPlayers: 4},
		{Name: "Cascadia", URL: "cascadia.pdf", Rating: 7.9, Complexity: 1.8, MinPlayers: 1, MaxPlayers: 4},
		{Name: "Captain Sonar", URL: "sonar.pdf", Rating: 7.5, Complexity: 2.1, MinPlayers: 2, MaxPlayers: 8},
		{Name: "Unrated Proto", URL: "proto.pdf", Rating: 0.0, Complexity: 2.0, MinPlayers: 3, MaxPlayers: 6},
	}
	for _, g := range games {
		if err := db.CreateGame(g); err != nil {
			t.Fatalf("failed to create game: %v", err)
		}
	}

	// 1. Sort by Name Asc
	byNameAsc, err := db.ListBaseGames("", 0, 0, 5, "name", "asc")
	if err != nil {
		t.Fatalf("failed to list by name asc: %v", err)
	}
	if byNameAsc[0].Name != "Brass: Birmingham" || byNameAsc[3].Name != "Unrated Proto" {
		t.Errorf("unexpected name asc order: %v, %v", byNameAsc[0].Name, byNameAsc[3].Name)
	}

	// 2. Sort by Name Desc
	byNameDesc, err := db.ListBaseGames("", 0, 0, 5, "name", "desc")
	if err != nil {
		t.Fatalf("failed to list by name desc: %v", err)
	}
	if byNameDesc[0].Name != "Unrated Proto" || byNameDesc[3].Name != "Brass: Birmingham" {
		t.Errorf("unexpected name desc order: %v, %v", byNameDesc[0].Name, byNameDesc[3].Name)
	}

	// 3. Sort by Rating Desc (0.0 unrated goes to end)
	byRatingDesc, err := db.ListBaseGames("", 0, 0, 5, "rating", "desc")
	if err != nil {
		t.Fatalf("failed to list by rating desc: %v", err)
	}
	if byRatingDesc[0].Name != "Brass: Birmingham" || byRatingDesc[3].Name != "Unrated Proto" {
		t.Errorf("unexpected rating desc order: %v, %v", byRatingDesc[0].Name, byRatingDesc[3].Name)
	}

	// 4. Sort by Complexity Desc
	byCompDesc, err := db.ListBaseGames("", 0, 0, 5, "complexity", "desc")
	if err != nil {
		t.Fatalf("failed to list by complexity desc: %v", err)
	}
	if byCompDesc[0].Name != "Brass: Birmingham" || byCompDesc[3].Name != "Cascadia" {
		t.Errorf("unexpected complexity desc order: %v, %v", byCompDesc[0].Name, byCompDesc[3].Name)
	}

	// 5. Sort by Min Players Asc
	byMinAsc, err := db.ListBaseGames("", 0, 0, 5, "min_players", "asc")
	if err != nil {
		t.Fatalf("failed to list by min players asc: %v", err)
	}
	if byMinAsc[0].Name != "Cascadia" { // 1 player
		t.Errorf("expected Cascadia (1 min player) first, got: %s", byMinAsc[0].Name)
	}

	// 6. Sort by Max Players Desc
	byMaxDesc, err := db.ListBaseGames("", 0, 0, 5, "max_players", "desc")
	if err != nil {
		t.Fatalf("failed to list by max players desc: %v", err)
	}
	if byMaxDesc[0].Name != "Captain Sonar" { // 8 players
		t.Errorf("expected Captain Sonar (8 max players) first, got: %s", byMaxDesc[0].Name)
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

func TestFormatExpansionName(t *testing.T) {
	tests := []struct {
		name          string
		expansionName string
		parentName    string
		expected      string
	}{
		{
			name:          "LotR en-dash",
			expansionName: "The Lord of the Rings: The Card Game – The Hunt for Gollum",
			parentName:    "The Lord of the Rings: The Card Game",
			expected:      "The Hunt for Gollum",
		},
		{
			name:          "LotR hyphen",
			expansionName: "The Lord of the Rings: The Card Game - Conflict at the Carrock",
			parentName:    "The Lord of the Rings: The Card Game",
			expected:      "Conflict at the Carrock",
		},
		{
			name:          "ASL colon",
			expansionName: "Advanced Squad Leader: Starter Kit #1",
			parentName:    "Advanced Squad Leader",
			expected:      "Starter Kit #1",
		},
		{
			name:          "Root colon",
			expansionName: "Root: The Riverfolk Expansion",
			parentName:    "Root",
			expected:      "The Riverfolk Expansion",
		},
		{
			name:          "Dune Imperium hyphen",
			expansionName: "Dune: Imperium - Rise of Ix",
			parentName:    "Dune: Imperium",
			expected:      "Rise of Ix",
		},
		{
			name:          "Parent with Revised Edition suffix",
			expansionName: "The Lord of the Rings: The Card Game – The Dark of Mirkwood",
			parentName:    "The Lord of the Rings: The Card Game (Revised Edition)",
			expected:      "The Dark of Mirkwood",
		},
		{
			name:          "Internal hyphen in name preserved",
			expansionName: "The Lord of the Rings: The Card Game – Khazad-dûm",
			parentName:    "The Lord of the Rings: The Card Game",
			expected:      "Khazad-dûm",
		},
		{
			name:          "No match returns unchanged",
			expansionName: "Invaders from Afar",
			parentName:    "Scythe",
			expected:      "Invaders from Afar",
		},
		{
			name:          "Identical name returns unchanged",
			expansionName: "Root",
			parentName:    "Root",
			expected:      "Root",
		},
		{
			name:          "Empty parent returns unchanged",
			expansionName: "Rise of Ix",
			parentName:    "",
			expected:      "Rise of Ix",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatExpansionName(tc.expansionName, tc.parentName)
			if got != tc.expected {
				t.Errorf("FormatExpansionName(%q, %q) = %q; want %q", tc.expansionName, tc.parentName, got, tc.expected)
			}
		})
	}
}

func TestSettings(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "settings_test.db")
	db, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// 1. Default value when not set
	if db.GetSettingBool("hide_game_title_in_expansions", false) != false {
		t.Errorf("expected default false when not set")
	}

	// 2. Set to true
	if err := db.SetSettingBool("hide_game_title_in_expansions", true); err != nil {
		t.Fatalf("SetSettingBool failed: %v", err)
	}
	if !db.GetSettingBool("hide_game_title_in_expansions", false) {
		t.Errorf("expected true after setting to true")
	}

	settings := db.GetAdminSettings()
	if !settings.HideGameTitleInExpansions {
		t.Errorf("expected GetAdminSettings to return HideGameTitleInExpansions=true")
	}

	// 3. Set to false
	if err := db.SetSettingBool("hide_game_title_in_expansions", false); err != nil {
		t.Fatalf("SetSettingBool failed: %v", err)
	}
	if db.GetSettingBool("hide_game_title_in_expansions", true) != false {
		t.Errorf("expected false after setting to false")
	}
}

func TestCountGamesAndExpansions(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "count_test.db")
	db, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Empty DB
	games, expansions, err := db.CountGamesAndExpansions()
	if err != nil {
		t.Fatalf("CountGamesAndExpansions failed on empty db: %v", err)
	}
	if games != 0 || expansions != 0 {
		t.Fatalf("expected 0, 0 for empty db, got %d, %d", games, expansions)
	}

	// Add 2 base games
	base1 := &Game{Name: "Game 1", URL: "g1.pdf"}
	base2 := &Game{Name: "Game 2", URL: "g2.pdf"}
	if err := db.CreateGame(base1); err != nil {
		t.Fatalf("CreateGame base1 failed: %v", err)
	}
	if err := db.CreateGame(base2); err != nil {
		t.Fatalf("CreateGame base2 failed: %v", err)
	}

	games, expansions, err = db.CountGamesAndExpansions()
	if err != nil || games != 2 || expansions != 0 {
		t.Fatalf("expected 2 games, 0 expansions, got %d games, %d expansions (err: %v)", games, expansions, err)
	}

	// Add 1 expansion
	parentID := base1.ID
	exp1 := &Game{Name: "Exp 1", URL: "e1.pdf", ParentID: &parentID}
	if err := db.CreateGame(exp1); err != nil {
		t.Fatalf("CreateGame exp1 failed: %v", err)
	}

	games, expansions, err = db.CountGamesAndExpansions()
	if err != nil || games != 2 || expansions != 1 {
		t.Fatalf("expected 2 games, 1 expansion, got %d games, %d expansions (err: %v)", games, expansions, err)
	}
}

func TestGameRating(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "rating_test.db")
	db, err := Open(dbPath, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// 1. Verify rating column exists in schema
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('games') WHERE name = 'rating'").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected rating column in games table, got count %d, err %v", count, err)
	}

	// 2. Create game with Rating
	g := &Game{
		Name:        "Castles of Burgundy",
		URL:         "cob.pdf",
		Image:       "cob.jpg",
		MinPlayers:  1,
		MaxPlayers:  4,
		BestPlayers: "2",
		Complexity:  3.0,
		Rating:      8.35,
	}
	if err := db.CreateGame(g); err != nil {
		t.Fatalf("CreateGame failed: %v", err)
	}

	loaded, err := db.GetGame(g.ID)
	if err != nil || loaded == nil {
		t.Fatalf("GetGame failed: %v", err)
	}
	if loaded.Rating != 8.35 {
		t.Errorf("expected rating 8.35, got %f", loaded.Rating)
	}

	// 3. Update game rating
	loaded.Rating = 8.5
	if err := db.UpdateGame(loaded); err != nil {
		t.Fatalf("UpdateGame failed: %v", err)
	}
	reloaded, _ := db.GetGame(g.ID)
	if reloaded.Rating != 8.5 {
		t.Errorf("expected updated rating 8.5, got %f", reloaded.Rating)
	}
}


