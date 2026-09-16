package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(dbPath string, seedJSONPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = DELETE;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			log.Printf("Warning: failed to execute %s: %v", p, err)
		}
	}

	wrapped := &DB{db}
	if err := wrapped.migrate(); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if seedJSONPath != "" {
		if err := wrapped.seedIfEmpty(seedJSONPath); err != nil {
			log.Printf("Warning: failed to seed database: %v", err)
		}
	}

	return wrapped, nil
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS games (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		parent_id INTEGER REFERENCES games(id) ON DELETE SET NULL,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		image TEXT NOT NULL,
		min_players INTEGER DEFAULT 1,
		max_players INTEGER DEFAULT 4,
		best_players TEXT DEFAULT '',
		complexity REAL DEFAULT 0.0,
		rating REAL DEFAULT 0.0,
		bgg_url TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_games_name ON games(name);

	CREATE TABLE IF NOT EXISTS game_documents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		title TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT 'core',
		filename TEXT NOT NULL,
		is_primary BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_game_documents_game_id ON game_documents(game_id);

	CREATE TABLE IF NOT EXISTS pdf_optimizations (
		filename TEXT PRIMARY KEY,
		file_size INTEGER NOT NULL,
		mod_time INTEGER NOT NULL,
		is_linearized INTEGER NOT NULL DEFAULT 0,
		optimized_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS user_games (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, game_id)
	);
	CREATE INDEX IF NOT EXISTS idx_user_games_user ON user_games(user_id);
	CREATE INDEX IF NOT EXISTS idx_user_games_game ON user_games(game_id);

	CREATE TABLE IF NOT EXISTS user_collections (
		user_id TEXT PRIMARY KEY,
		custom_name TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Dynamic column migrations for existing SQLite databases
	var hasBestPlayers, hasComplexity, hasRating, hasParentID, hasBggID bool
	rows, err := db.Query("PRAGMA table_info(games);")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
			if name == "best_players" {
				hasBestPlayers = true
			}
			if name == "complexity" {
				hasComplexity = true
			}
			if name == "rating" {
				hasRating = true
			}
			if name == "parent_id" {
				hasParentID = true
			}
			if name == "bgg_id" {
				hasBggID = true
			}
		}
	}

	if !hasBestPlayers {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN best_players TEXT DEFAULT '';")
	}
	if !hasComplexity {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN complexity REAL DEFAULT 0.0;")
	}
	if !hasRating {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN rating REAL DEFAULT 0.0;")
	}
	if !hasParentID {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN parent_id INTEGER REFERENCES games(id) ON DELETE SET NULL;")
	}
	if !hasBggID {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN bgg_id INTEGER;")
	}
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_games_parent_id ON games(parent_id);")
	_, _ = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_games_bgg_id ON games(bgg_id) WHERE bgg_id IS NOT NULL;")

	// Backfill bgg_id from existing bgg_url if bgg_id IS NULL
	db.backfillBggIDs()

	// Backfill game_documents from existing games.url if empty
	backfill := `
	INSERT INTO game_documents (game_id, title, category, filename, is_primary, created_at)
	SELECT g.id, 'Core Rulebook', 'core', g.url, 1, g.created_at
	FROM games g
	WHERE g.url != '' AND NOT EXISTS (
		SELECT 1 FROM game_documents gd WHERE gd.game_id = g.id
	);`
	_, _ = db.Exec(backfill)

	// Backfill user_games for default owner ('cdk2128') if user_games is empty but games exist
	db.backfillDefaultOwner("cdk2128")

	return nil
}

func (db *DB) backfillBggIDs() {
	rows, err := db.Query("SELECT id, bgg_url FROM games WHERE bgg_id IS NULL AND bgg_url != ''")
	if err != nil {
		return
	}
	defer rows.Close()

	var updates []struct {
		id    int64
		bggID int
	}
	for rows.Next() {
		var id int64
		var rawURL string
		if err := rows.Scan(&id, &rawURL); err == nil {
			if bggID, ok := extractBggID(rawURL); ok {
				updates = append(updates, struct {
					id    int64
					bggID int
				}{id, bggID})
			}
		}
	}

	for _, u := range updates {
		// Use INSERT OR IGNORE / try-update so duplicate BGG IDs don't violate unique constraint
		_, _ = db.Exec("UPDATE games SET bgg_id = ? WHERE id = ? AND NOT EXISTS (SELECT 1 FROM games g2 WHERE g2.bgg_id = ? AND g2.id != ?)",
			u.bggID, u.id, u.bggID, u.id)
	}
}

func extractBggID(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	markers := []string{"/boardgame/", "/boardgameexpansion/"}
	lower := strings.ToLower(raw)
	for _, marker := range markers {
		if idx := strings.Index(lower, marker); idx != -1 {
			rest := raw[idx+len(marker):]
			parts := strings.Split(rest, "/")
			if len(parts) > 0 {
				var id int
				if _, err := fmt.Sscanf(parts[0], "%d", &id); err == nil && id > 0 {
					return id, true
				}
			}
		}
	}
	var id int
	if _, err := fmt.Sscanf(raw, "%d", &id); err == nil && id > 0 {
		return id, true
	}
	return 0, false
}

func (db *DB) backfillDefaultOwner(defaultOwner string) {
	if defaultOwner == "" {
		defaultOwner = "cdk2128"
	}
	var ugCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM user_games").Scan(&ugCount)
	if ugCount == 0 {
		_, _ = db.Exec("INSERT OR IGNORE INTO user_games (user_id, game_id) SELECT ?, id FROM games", defaultOwner)
	}
}

func (db *DB) seedIfEmpty(seedPath string) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	data, err := os.ReadFile(seedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var rawGames []struct {
		ID          int     `json:"id"`
		ParentID    *int64  `json:"parent_id"`
		Name        string  `json:"name"`
		URL         string  `json:"url"`
		Image       string  `json:"image"`
		MinPlayers  int     `json:"min_players"`
		MaxPlayers  int     `json:"max_players"`
		BestPlayers string  `json:"best_players"`
		Complexity  float64 `json:"complexity"`
		Rating      float64 `json:"rating"`
		BggURL      string  `json:"bgg_url"`
		Documents   []struct {
			Title     string `json:"title"`
			Category  string `json:"category"`
			Filename  string `json:"filename"`
			IsPrimary bool   `json:"is_primary"`
		} `json:"documents"`
	}

	if err := json.Unmarshal(data, &rawGames); err != nil {
		return fmt.Errorf("parsing seed games json: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO games (id, parent_id, name, url, image, min_players, max_players, best_players, complexity, rating, bgg_url, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	docStmt, err := tx.Prepare(`INSERT INTO game_documents (game_id, title, category, filename, is_primary, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer docStmt.Close()

	now := time.Now().UTC()
	for _, g := range rawGames {
		if _, err := stmt.Exec(g.ID, g.ParentID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.Rating, g.BggURL, now, now); err != nil {
			return err
		}

		if len(g.Documents) > 0 {
			for _, doc := range g.Documents {
				if _, err := docStmt.Exec(g.ID, doc.Title, doc.Category, doc.Filename, doc.IsPrimary, now); err != nil {
					return err
				}
			}
		} else if g.URL != "" {
			if _, err := docStmt.Exec(g.ID, "Core Rulebook", "core", g.URL, true, now); err != nil {
				return err
			}
		}
	}

	log.Printf("Seeded %d games into database from %s", len(rawGames), seedPath)
	return tx.Commit()
}

func (db *DB) ListGames(search string, players int) ([]Game, error) {
	query := "SELECT id, bgg_id, parent_id, name, url, image, min_players, max_players, best_players, complexity, rating, bgg_url, created_at, updated_at FROM games WHERE 1=1"
	var args []interface{}

	search = strings.TrimSpace(search)
	if search != "" {
		query += " AND (name LIKE ? OR url LIKE ?)"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}

	if players > 0 {
		query += " AND min_players <= ? AND max_players >= ?"
		args = append(args, players, players)
	}

	query += " ORDER BY name COLLATE NOCASE ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var g Game
		var bggID sql.NullInt64
		var parentID sql.NullInt64
		if err := rows.Scan(&g.ID, &bggID, &parentID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.Rating, &g.BggURL, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		if bggID.Valid {
			bid := int(bggID.Int64)
			g.BggID = &bid
		}
		if parentID.Valid {
			pid := parentID.Int64
			g.ParentID = &pid
		}
		games = append(games, g)
	}

	return games, rows.Err()
}

// ListBaseGames returns only base games (parent_id IS NULL), optionally filtered by a specific collectionUser.
// If collectionUser is empty, or "all", it searches across all library games.
func (db *DB) ListBaseGames(search string, players int, minComplexity, maxComplexity float64, sortBy, sortOrder, collectionUser string) ([]Game, error) {
	query := `SELECT g.id, g.bgg_id, g.parent_id, g.name, g.url, g.image, g.min_players, g.max_players, g.best_players, g.complexity, g.rating, g.bgg_url, g.created_at, g.updated_at
		FROM games g WHERE g.parent_id IS NULL`
	var args []interface{}

	collectionUser = strings.TrimSpace(collectionUser)
	if collectionUser != "" && !strings.EqualFold(collectionUser, "all") {
		query += ` AND EXISTS (SELECT 1 FROM user_games ug WHERE ug.game_id = g.id AND ug.user_id = ?)`
		args = append(args, collectionUser)
	}

	search = strings.TrimSpace(search)
	if search != "" {
		like := "%" + search + "%"
		query += ` AND (g.name LIKE ? OR g.url LIKE ? OR EXISTS (
			SELECT 1 FROM games e WHERE e.parent_id = g.id AND (e.name LIKE ? OR e.url LIKE ?)
		))`
		args = append(args, like, like, like, like)
	}

	if players > 0 {
		query += " AND g.min_players <= ? AND g.max_players >= ?"
		args = append(args, players, players)
	}

	if minComplexity > 0.0 || (maxComplexity > 0.0 && maxComplexity < 5.0) {
		if maxComplexity <= 0.0 {
			maxComplexity = 5.0
		}
		query += " AND g.complexity >= ? AND g.complexity <= ?"
		args = append(args, minComplexity, maxComplexity)
	}

	orderDir := "ASC"
	if strings.ToLower(sortOrder) == "desc" {
		orderDir = "DESC"
	}

	var sortExpr string
	switch strings.ToLower(sortBy) {
	case "rating":
		if orderDir == "DESC" {
			sortExpr = "CASE WHEN g.rating > 0 THEN 0 ELSE 1 END, g.rating DESC, g.name COLLATE NOCASE ASC"
		} else {
			sortExpr = "CASE WHEN g.rating > 0 THEN 0 ELSE 1 END, g.rating ASC, g.name COLLATE NOCASE ASC"
		}
	case "complexity":
		if orderDir == "DESC" {
			sortExpr = "CASE WHEN g.complexity > 0 THEN 0 ELSE 1 END, g.complexity DESC, g.name COLLATE NOCASE ASC"
		} else {
			sortExpr = "CASE WHEN g.complexity > 0 THEN 0 ELSE 1 END, g.complexity ASC, g.name COLLATE NOCASE ASC"
		}
	case "min_players":
		sortExpr = fmt.Sprintf("g.min_players %s, g.name COLLATE NOCASE ASC", orderDir)
	case "max_players":
		sortExpr = fmt.Sprintf("g.max_players %s, g.name COLLATE NOCASE ASC", orderDir)
	case "name":
		fallthrough
	default:
		sortExpr = fmt.Sprintf("g.name COLLATE NOCASE %s", orderDir)
	}

	query += " ORDER BY " + sortExpr

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var g Game
		var bggID sql.NullInt64
		var parentID sql.NullInt64
		if err := rows.Scan(&g.ID, &bggID, &parentID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.Rating, &g.BggURL, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		if bggID.Valid {
			bid := int(bggID.Int64)
			g.BggID = &bid
		}
		if parentID.Valid {
			pid := parentID.Int64
			g.ParentID = &pid
		}
		games = append(games, g)
	}

	return games, rows.Err()
}

// CountGamesAndExpansions returns the total count of base games (parent_id IS NULL)
// and expansions (parent_id IS NOT NULL) in the library or specific collection.
func (db *DB) CountGamesAndExpansions(collectionUser string) (int, int, error) {
	collectionUser = strings.TrimSpace(collectionUser)
	query := `
		SELECT 
			COALESCE(SUM(CASE WHEN g.parent_id IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN g.parent_id IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM games g`
	var args []interface{}

	if collectionUser != "" && !strings.EqualFold(collectionUser, "all") {
		query += ` WHERE EXISTS (SELECT 1 FROM user_games ug WHERE ug.game_id = g.id AND ug.user_id = ?)`
		args = append(args, collectionUser)
	}

	var games, expansions int
	err := db.QueryRow(query, args...).Scan(&games, &expansions)
	if err != nil {
		return 0, 0, err
	}
	return games, expansions, nil
}

func (db *DB) GetGame(id int64) (*Game, error) {
	var g Game
	var bggID sql.NullInt64
	var parentID sql.NullInt64
	err := db.QueryRow("SELECT id, bgg_id, parent_id, name, url, image, min_players, max_players, best_players, complexity, rating, bgg_url, created_at, updated_at FROM games WHERE id = ?", id).
		Scan(&g.ID, &bggID, &parentID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.Rating, &g.BggURL, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if bggID.Valid {
		bid := int(bggID.Int64)
		g.BggID = &bid
	}
	if parentID.Valid {
		pid := parentID.Int64
		g.ParentID = &pid
	}
	return &g, nil
}

// FindGameByBggID finds a game in the master library by its BoardGameGeek ID
func (db *DB) FindGameByBggID(bggID int) (*Game, error) {
	if bggID <= 0 {
		return nil, nil
	}
	var g Game
	var bID sql.NullInt64
	var parentID sql.NullInt64
	err := db.QueryRow("SELECT id, bgg_id, parent_id, name, url, image, min_players, max_players, best_players, complexity, rating, bgg_url, created_at, updated_at FROM games WHERE bgg_id = ?", bggID).
		Scan(&g.ID, &bID, &parentID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.Rating, &g.BggURL, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if bID.Valid {
		bid := int(bID.Int64)
		g.BggID = &bid
	}
	if parentID.Valid {
		pid := parentID.Int64
		g.ParentID = &pid
	}
	return &g, nil
}

func (db *DB) ListDocuments(gameID int64) ([]GameDocument, error) {
	rows, err := db.Query(`SELECT id, game_id, title, category, filename, is_primary, created_at 
		FROM game_documents WHERE game_id = ? ORDER BY is_primary DESC, id ASC`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []GameDocument
	for rows.Next() {
		var d GameDocument
		if err := rows.Scan(&d.ID, &d.GameID, &d.Title, &d.Category, &d.Filename, &d.IsPrimary, &d.CreatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func (db *DB) AddDocument(doc *GameDocument) error {
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO game_documents (game_id, title, category, filename, is_primary, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, doc.GameID, doc.Title, doc.Category, doc.Filename, doc.IsPrimary, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		doc.ID = id
	}
	doc.CreatedAt = now
	return nil
}

func (db *DB) DeleteDocument(id int64) error {
	_, err := db.Exec("DELETE FROM game_documents WHERE id = ?", id)
	return err
}

func (db *DB) ListExpansions(parentID int64) ([]Game, error) {
	rows, err := db.Query(`SELECT id, bgg_id, parent_id, name, url, image, min_players, max_players, best_players, complexity, rating, bgg_url, created_at, updated_at 
		FROM games WHERE parent_id = ? ORDER BY name COLLATE NOCASE ASC`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expansions []Game
	for rows.Next() {
		var g Game
		var bggID sql.NullInt64
		var pID sql.NullInt64
		if err := rows.Scan(&g.ID, &bggID, &pID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.Rating, &g.BggURL, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		if bggID.Valid {
			bid := int(bggID.Int64)
			g.BggID = &bid
		}
		if pID.Valid {
			pid := pID.Int64
			g.ParentID = &pid
		}
		expansions = append(expansions, g)
	}
	return expansions, rows.Err()
}

func (db *DB) GetGameWithDetails(id int64) (*Game, []GameDocument, []Game, error) {
	game, err := db.GetGame(id)
	if err != nil || game == nil {
		return nil, nil, nil, err
	}

	docs, err := db.ListDocuments(id)
	if err != nil {
		return nil, nil, nil, err
	}

	expansions, err := db.ListExpansions(id)
	if err != nil {
		return nil, nil, nil, err
	}

	return game, docs, expansions, nil
}

func (db *DB) CreateGame(g *Game) error {
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO games (bgg_id, parent_id, name, url, image, min_players, max_players, best_players, complexity, rating, bgg_url, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.BggID, g.ParentID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.Rating, g.BggURL, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		g.ID = id
	}
	g.CreatedAt = now
	g.UpdatedAt = now

	// If g.URL is provided, also create initial primary document
	if g.URL != "" {
		_ = db.AddDocument(&GameDocument{
			GameID:    g.ID,
			Title:     "Core Rulebook",
			Category:  "core",
			Filename:  g.URL,
			IsPrimary: true,
		})
	}

	return nil
}

func (db *DB) UpdateGame(g *Game) error {
	now := time.Now().UTC()
	_, err := db.Exec(`UPDATE games SET bgg_id = ?, parent_id = ?, name = ?, url = ?, image = ?, min_players = ?, max_players = ?, best_players = ?, complexity = ?, rating = ?, bgg_url = ?, updated_at = ? WHERE id = ?`,
		g.BggID, g.ParentID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.Rating, g.BggURL, now, g.ID)
	if err == nil {
		g.UpdatedAt = now
	}
	return err
}

func (db *DB) DeleteGame(id int64) error {
	_, err := db.Exec("DELETE FROM games WHERE id = ?", id)
	return err
}

// User Collection Management

func (db *DB) AddGameToUserCollection(userID string, gameID int64) error {
	userID = strings.TrimSpace(userID)
	if userID == "" || gameID <= 0 {
		return errors.New("invalid user ID or game ID")
	}
	_, err := db.Exec("INSERT OR IGNORE INTO user_games (user_id, game_id) VALUES (?, ?)", userID, gameID)
	return err
}

func (db *DB) RemoveGameFromUserCollection(userID string, gameID int64) error {
	userID = strings.TrimSpace(userID)
	if userID == "" || gameID <= 0 {
		return errors.New("invalid user ID or game ID")
	}
	_, err := db.Exec("DELETE FROM user_games WHERE user_id = ? AND game_id = ?", userID, gameID)
	return err
}

func (db *DB) IsGameInUserCollection(userID string, gameID int64) (bool, error) {
	var exists int
	err := db.QueryRow("SELECT 1 FROM user_games WHERE user_id = ? AND game_id = ?", userID, gameID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return exists == 1, err
}

func truncateName(name string, maxLen int) string {
	runes := []rune(name)
	if len(runes) <= maxLen {
		return name
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// GetCollectionName returns the custom name of the user's collection, or their userID if not set
func (db *DB) GetCollectionName(userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	var customName string
	err := db.QueryRow("SELECT custom_name FROM user_collections WHERE user_id = ?", userID).Scan(&customName)
	if errors.Is(err, sql.ErrNoRows) || customName == "" {
		return userID, nil
	}
	return customName, err
}

// SetCollectionName updates or clears a custom name for a user's collection (max 30 characters)
func (db *DB) SetCollectionName(userID, customName string) error {
	if userID == "" {
		return fmt.Errorf("user ID cannot be empty")
	}
	customName = strings.TrimSpace(customName)
	runes := []rune(customName)
	if len(runes) > 30 {
		customName = string(runes[:30])
	}

	if customName == "" || customName == userID {
		_, err := db.Exec("DELETE FROM user_collections WHERE user_id = ?", userID)
		return err
	}

	_, err := db.Exec(`
		INSERT INTO user_collections (user_id, custom_name, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET custom_name=excluded.custom_name, updated_at=CURRENT_TIMESTAMP
	`, userID, customName)
	return err
}

func (db *DB) ListAllCollections() ([]CollectionOption, error) {
	rows, err := db.Query(`
		SELECT DISTINCT u.user_id, COALESCE(c.custom_name, '')
		FROM user_games u
		LEFT JOIN user_collections c ON u.user_id = c.user_id
		ORDER BY u.user_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var colls []CollectionOption
	for rows.Next() {
		var u, custom string
		if err := rows.Scan(&u, &custom); err == nil && u != "" {
			displayName := strings.TrimSpace(custom)
			if displayName == "" {
				displayName = u
			}
			colls = append(colls, CollectionOption{
				UserID:        u,
				DisplayName:   displayName,
				TruncatedName: truncateName(displayName, 19),
			})
		}
	}
	return colls, rows.Err()
}

// GetGameSharedUserCount checks which other users have this game in their collection
func (db *DB) GetGameSharedUserCount(gameID int64, excludeUserID string) (int, []string, error) {
	rows, err := db.Query("SELECT DISTINCT user_id FROM user_games WHERE game_id = ? AND user_id != ?", gameID, excludeUserID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil && u != "" {
			users = append(users, u)
		}
	}
	return len(users), users, rows.Err()
}

// UpdateGameParent moves a game under a new parent or promotes it to root (newParentID == nil)
func (db *DB) UpdateGameParent(gameID int64, newParentID *int64) error {
	now := time.Now().UTC()
	_, err := db.Exec("UPDATE games SET parent_id = ?, updated_at = ? WHERE id = ?", newParentID, now, gameID)
	return err
}


type PDFOptimization struct {
	Filename     string    `json:"filename"`
	FileSize     int64     `json:"file_size"`
	ModTime      int64     `json:"mod_time"`
	IsLinearized bool      `json:"is_linearized"`
	OptimizedAt  time.Time `json:"optimized_at"`
}

func (db *DB) GetPDFOptimization(filename string) (*PDFOptimization, error) {
	row := db.QueryRow(`SELECT filename, file_size, mod_time, is_linearized, optimized_at FROM pdf_optimizations WHERE filename = ?`, filename)
	var opt PDFOptimization
	var isLinearized int
	var optAt time.Time
	err := row.Scan(&opt.Filename, &opt.FileSize, &opt.ModTime, &isLinearized, &optAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	opt.IsLinearized = isLinearized == 1
	opt.OptimizedAt = optAt
	return &opt, nil
}

func (db *DB) SavePDFOptimization(opt *PDFOptimization) error {
	isLin := 0
	if opt.IsLinearized {
		isLin = 1
	}
	query := `
	INSERT INTO pdf_optimizations (filename, file_size, mod_time, is_linearized, optimized_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(filename) DO UPDATE SET
		file_size=excluded.file_size,
		mod_time=excluded.mod_time,
		is_linearized=excluded.is_linearized,
		optimized_at=CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, opt.Filename, opt.FileSize, opt.ModTime, isLin)
	return err
}

func (db *DB) GetOptimizationStats() (total, linearized, pending int, err error) {
	row := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_linearized = 1 THEN 1 ELSE 0 END), 0) FROM pdf_optimizations`)
	err = row.Scan(&total, &linearized)
	if err != nil {
		return 0, 0, 0, err
	}
	pending = total - linearized
	return total, linearized, pending, nil
}

func (db *DB) GetSetting(key, defaultValue string) (string, error) {
	var val string
	err := db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultValue, nil
		}
		return defaultValue, err
	}
	return val, nil
}

func (db *DB) GetSettingBool(key string, defaultValue bool) bool {
	val, err := db.GetSetting(key, "")
	if err != nil || val == "" {
		return defaultValue
	}
	return val == "1" || strings.ToLower(val) == "true" || val == "on"
}

func (db *DB) SetSetting(key, value string) error {
	query := `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value;`
	_, err := db.Exec(query, key, value)
	return err
}

func (db *DB) SetSettingBool(key string, value bool) error {
	strVal := "0"
	if value {
		strVal = "1"
	}
	return db.SetSetting(key, strVal)
}

func (db *DB) GetAdminSettings() AdminSettings {
	defColl, _ := db.GetSetting("default_collection", "")
	bggToken, _ := db.GetSetting("bgg_api_token", "")
	return AdminSettings{
		HideGameTitleInExpansions: db.GetSettingBool("hide_game_title_in_expansions", false),
		DefaultCollection:         defColl,
		RestrictSharedGameMoves:   db.GetSettingBool("restrict_shared_game_moves", false),
		BGGApiToken:               bggToken,
	}
}

// FormatExpansionName strips the parent game title and leading separators
// (such as " – ", ": ", " - ", " — ") from an expansion's name.
func FormatExpansionName(expansionName, parentName string) string {
	if parentName == "" || strings.TrimSpace(expansionName) == "" {
		return expansionName
	}

	expLower := strings.ToLower(expansionName)
	candidates := []string{parentName}

	// Also check parentName without trailing parenthetical (e.g. "Game (Revised Edition)" -> "Game")
	if idx := strings.LastIndex(parentName, " ("); idx != -1 && strings.HasSuffix(parentName, ")") {
		clean := strings.TrimSpace(parentName[:idx])
		if clean != "" {
			candidates = append(candidates, clean)
		}
	}

	for _, cand := range candidates {
		candLower := strings.ToLower(cand)
		if strings.HasPrefix(expLower, candLower) {
			trimmed := expansionName[len(cand):]
			// Trim leading separators (whitespace, colons, hyphens, en-dashes, em-dashes, slashes)
			trimmed = strings.TrimLeft(trimmed, " \t\r\n:-–—/")
			trimmed = strings.TrimSpace(trimmed)
			if trimmed != "" {
				return trimmed
			}
		}
	}

	return expansionName
}

