package database

import "time"

type Game struct {
	ID          int64     `json:"id"`
	ParentID    *int64    `json:"parent_id,omitempty"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name,omitempty"`
	URL         string    `json:"url"`
	Image       string    `json:"image"`
	MinPlayers  int       `json:"min_players"`
	MaxPlayers  int       `json:"max_players"`
	BestPlayers string    `json:"best_players"`
	Complexity  float64   `json:"complexity"`
	BggURL      string    `json:"bgg_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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
	HideGameTitleInExpansions bool `json:"hide_game_title_in_expansions"`
}
