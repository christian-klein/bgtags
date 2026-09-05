package database

import (
	"database/sql"
	"encoding/json"
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
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Dynamic column migrations for existing SQLite databases
	var hasBestPlayers, hasComplexity bool
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
		}
	}

	if !hasBestPlayers {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN best_players TEXT DEFAULT '';")
	}
	if !hasComplexity {
		_, _ = db.Exec("ALTER TABLE games ADD COLUMN complexity REAL DEFAULT 0.0;")
	}

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
		Name        string  `json:"name"`
		URL         string  `json:"url"`
		Image       string  `json:"image"`
		MinPlayers  int     `json:"min_players"`
		MaxPlayers  int     `json:"max_players"`
		BestPlayers string  `json:"best_players"`
		Complexity  float64 `json:"complexity"`
		BggURL      string  `json:"bgg_url"`
	}

	if err := json.Unmarshal(data, &rawGames); err != nil {
		return fmt.Errorf("parsing seed games json: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO games (id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, g := range rawGames {
		if _, err := stmt.Exec(g.ID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.BggURL, now, now); err != nil {
			return err
		}
	}

	log.Printf("Seeded %d games into database from %s", len(rawGames), seedPath)
	return tx.Commit()
}

func (db *DB) ListGames(search string, players int) ([]Game, error) {
	query := "SELECT id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at FROM games WHERE 1=1"
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
		if err := rows.Scan(&g.ID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.BggURL, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		games = append(games, g)
	}

	return games, rows.Err()
}

func (db *DB) GetGame(id int64) (*Game, error) {
	var g Game
	err := db.QueryRow("SELECT id, name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at FROM games WHERE id = ?", id).
		Scan(&g.ID, &g.Name, &g.URL, &g.Image, &g.MinPlayers, &g.MaxPlayers, &g.BestPlayers, &g.Complexity, &g.BggURL, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &g, nil
}

func (db *DB) CreateGame(g *Game) error {
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO games (name, url, image, min_players, max_players, best_players, complexity, bgg_url, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.BggURL, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		g.ID = id
	}
	g.CreatedAt = now
	g.UpdatedAt = now
	return nil
}

func (db *DB) UpdateGame(g *Game) error {
	now := time.Now().UTC()
	_, err := db.Exec(`UPDATE games SET name = ?, url = ?, image = ?, min_players = ?, max_players = ?, best_players = ?, complexity = ?, bgg_url = ?, updated_at = ? WHERE id = ?`,
		g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BestPlayers, g.Complexity, g.BggURL, now, g.ID)
	if err == nil {
		g.UpdatedAt = now
	}
	return err
}

func (db *DB) DeleteGame(id int64) error {
	_, err := db.Exec("DELETE FROM games WHERE id = ?", id)
	return err
}
