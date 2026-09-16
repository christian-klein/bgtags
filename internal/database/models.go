package database

import "time"

type Game struct {
	ID          int64     `json:"id"`
	BggID       *int      `json:"bgg_id,omitempty"`
	ParentID    *int64    `json:"parent_id,omitempty"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name,omitempty"`
	URL         string    `json:"url"`
	Image       string    `json:"image"`
	MinPlayers  int       `json:"min_players"`
	MaxPlayers  int       `json:"max_players"`
	BestPlayers string    `json:"best_players"`
	Complexity  float64   `json:"complexity"`
	Rating      float64   `json:"rating,omitempty"`
	BggURL      string    `json:"bgg_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UserGame struct {
	ID      int64     `json:"id"`
	UserID  string    `json:"user_id"`
	GameID  int64     `json:"game_id"`
	AddedAt time.Time `json:"added_at"`
}

type GameDocument struct {
	ID        int64     `json:"id"`
	GameID    int64     `json:"game_id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"` // 'core', 'glossary', 'expansion', 'faq', 'reference'
	Filename  string    `json:"filename"`
	IsPrimary bool      `json:"is_primary"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminSettings struct {
	HideGameTitleInExpansions bool   `json:"hide_game_title_in_expansions"`
	DefaultCollection         string `json:"default_collection"`
	RestrictSharedGameMoves   bool   `json:"restrict_shared_game_moves"`
	BGGApiToken               string `json:"bgg_api_token"`
}

type CollectionOption struct {
	UserID        string `json:"user_id"`
	DisplayName   string `json:"display_name"`
	TruncatedName string `json:"truncated_name"`
}
