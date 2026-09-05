package database

import "time"

type Game struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
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
