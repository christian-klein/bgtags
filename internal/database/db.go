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
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Dynamic column migrations for existing SQLite databases
	var hasBestPlayers, hasComplexity, hasParentID bool
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
			if name == "parent_id" {
				hasParentID = true
			}
		}
	}

	if !hasBestPlayers {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN best_players TEXT DEFAULT '';")
	}
	if !hasComplexity {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN complexity REAL DEFAULT 0.0;")
	}
	if !hasParentID {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN parent_id INTEGER REFERENCES games(id) ON DELETE SET NULL;")
	}
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_games_parent_id ON games(parent_id);")

	// Backfill game_documents from existing games.url if empty
	backfill := `
	INSERT INTO game_documents (game_id, title, category, filename, is_primary, created_at)
	SELECT g.id, 'Core Rulebook', 'core', g.url, 1, g.created_at
	FROM games g
	WHERE g.url != '' AND NOT EXISTS (
		SELECT 1 FROM game_documents gd WHERE gd.game_id = g.id
	);`
	_, _ = db.Exec(backfill)

	return nil
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

	stmt, err := tx.Prepare(`INSERT INTO games (id, parent_id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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
		if _, err := stmt.Exec(g.ID, g.ParentID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.BggURL, now, now); err != nil {
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
	query := "SELECT id, parent_id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at FROM games WHERE 1=1"
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
		var parentID sql.NullInt64
		if err := rows.Scan(&g.ID, &parentID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.BggURL, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		if parentID.Valid {
			pid := parentID.Int64
			g.ParentID = &pid
		}
		games = append(games, g)
	}

	return games, rows.Err()
}

// ListBaseGames returns only base games (parent_id IS NULL). A base game is
// included when it matches the search/player filter on its own fields OR when
// one of its expansions matches, so searching for an expansion surfaces its
// parent entry. Expansions themselves are grouped under parents by the handler.
func (db *DB) ListBaseGames(search string, players int, minComplexity, maxComplexity float64) ([]Game, error) {
	query := `SELECT g.id, g.parent_id, g.name, g.url, g.image, g.min_players, g.max_players, g.best_players, g.complexity, g.bgg_url, g.created_at, g.updated_at
		FROM games g WHERE g.parent_id IS NULL`
	var args []interface{}

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

	query += " ORDER BY g.name COLLATE NOCASE ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var g Game
		var parentID sql.NullInt64
		if err := rows.Scan(&g.ID, &parentID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.BggURL, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		games = append(games, g)
	}

	return games, rows.Err()
}

func (db *DB) GetGame(id int64) (*Game, error) {
	var g Game
	var parentID sql.NullInt64
	err := db.QueryRow("SELECT id, parent_id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at FROM games WHERE id = ?", id).
		Scan(&g.ID, &parentID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.BggURL, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
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
	rows, err := db.Query(`SELECT id, parent_id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at 
		FROM games WHERE parent_id = ? ORDER BY name COLLATE NOCASE ASC`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expansions []Game
	for rows.Next() {
		var g Game
		var pID sql.NullInt64
		if err := rows.Scan(&g.ID, &pID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.BggURL, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
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
	res, err := db.Exec(`INSERT INTO games (parent_id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ParentID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.BggURL, now, now)
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
	_, err := db.Exec(`UPDATE games SET parent_id = ?, name = ?, url = ?, image = ?, min_players = ?, max_players = ?, best_players = ?, complexity = ?, bgg_url = ?, updated_at = ? WHERE id = ?`,
		g.ParentID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.BggURL, now, g.ID)
	if err == nil {
		g.UpdatedAt = now
	}
	return err
}

func (db *DB) DeleteGame(id int64) error {
	_, err := db.Exec("DELETE FROM games WHERE id = ?", id)
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
	return AdminSettings{
		HideGameTitleInExpansions: db.GetSettingBool("hide_game_title_in_expansions", false),
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

