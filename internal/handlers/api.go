package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/christian-klein/bgtags/internal/database"
)

const (
	apiDefaultLimit = 50
	apiMaxLimit     = 200
	apiMaxLookupIDs = 200
)

// APIGame is the public JSON representation of a game served by /api/v1.
type APIGame struct {
	ID          int64   `json:"id"`
	BggID       *int    `json:"bgg_id"`
	Name        string  `json:"name"`
	ImageURL    string  `json:"image_url"`
	MinPlayers  int     `json:"min_players"`
	MaxPlayers  int     `json:"max_players"`
	BestPlayers string  `json:"best_players"`
	Complexity  float64 `json:"complexity"`
	Rating      float64 `json:"rating"`
	BggURL      string  `json:"bgg_url"`
	RulesURL    string  `json:"rules_url"`
	QRURL       string  `json:"qr_url"`
}

// APICollection is the public JSON representation of a user collection.
type APICollection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// apiToken returns the configured API bearer token: the API_TOKEN env var if
// set, otherwise the api_token admin setting. Empty means the API is disabled.
func (h *Handler) apiToken() string {
	if h.cfg.APIToken != "" {
		return h.cfg.APIToken
	}
	tok, _ := h.db.GetSetting("api_token", "")
	return strings.TrimSpace(tok)
}

func writeAPIJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("API JSON encode error: %v", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeAPIJSON(w, status, map[string]string{"error": msg})
}

// HandleAPI serves the read-only JSON API under /api/v1/. Authentication is a
// bearer token (not OIDC); the API answers 404 when no token is configured.
func (h *Handler) HandleAPI(w http.ResponseWriter, r *http.Request) {
	token := h.apiToken()
	if token == "" {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}

	provided, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(provided)), []byte(token)) != 1 {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch strings.TrimRight(r.URL.Path, "/") {
	case "/api/v1/collections":
		h.apiCollections(w, r)
	case "/api/v1/games":
		h.apiGames(w, r)
	case "/api/v1/games/lookup":
		h.apiLookup(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) apiCollections(w http.ResponseWriter, r *http.Request) {
	colls, err := h.db.ListAllCollections()
	if err != nil {
		log.Printf("API: error listing collections: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]APICollection, 0, len(colls))
	for _, c := range colls {
		out = append(out, APICollection{ID: c.UserID, Name: c.DisplayName})
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"collections": out})
}

func (h *Handler) apiGames(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := apiDefaultLimit
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeAPIError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, apiMaxLimit)
	}

	games, err := h.db.SearchGames(q.Get("q"), q.Get("collection"), limit)
	if err != nil {
		log.Printf("API: error searching games: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"games": h.toAPIGames(games, r)})
}

func (h *Handler) apiLookup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var bggIDs []int
	var ids []int64
	total := 0
	for _, s := range splitIDs(q["bgg_id"]) {
		if total >= apiMaxLookupIDs {
			break
		}
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			bggIDs = append(bggIDs, n)
			total++
		}
	}
	for _, s := range splitIDs(q["id"]) {
		if total >= apiMaxLookupIDs {
			break
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			ids = append(ids, n)
			total++
		}
	}
	if total == 0 {
		writeAPIError(w, http.StatusBadRequest, "no valid bgg_id or id values")
		return
	}

	games, err := h.db.LookupGames(bggIDs, ids)
	if err != nil {
		log.Printf("API: error looking up games: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"games": h.toAPIGames(games, r)})
}

// splitIDs flattens repeated and comma-separated query values.
func splitIDs(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (h *Handler) toAPIGames(games []database.Game, r *http.Request) []APIGame {
	baseURL := h.getBaseURL(r)
	out := make([]APIGame, 0, len(games))
	for _, g := range games {
		rulesURL := fmt.Sprintf("%s/games/%d/rules", baseURL, g.ID)
		imageURL := ""
		if g.Image != "" {
			imageURL = baseURL + "/img/" + url.PathEscape(g.Image)
		}
		out = append(out, APIGame{
			ID:          g.ID,
			BggID:       g.BggID,
			Name:        g.Name,
			ImageURL:    imageURL,
			MinPlayers:  g.MinPlayers,
			MaxPlayers:  g.MaxPlayers,
			BestPlayers: g.BestPlayers,
			Complexity:  g.Complexity,
			Rating:      g.Rating,
			BggURL:      g.BggURL,
			RulesURL:    rulesURL,
			QRURL:       baseURL + "/qr?url=" + url.QueryEscape(rulesURL),
		})
	}
	return out
}

// generateAPIToken returns a random 32-byte URL-safe (unpadded base64) token.
func generateAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HandleAdminAPIToken generates (action=generate) or clears (action=clear) the
// api_token admin setting. A freshly generated token is shown in full once in
// the response; afterwards the admin page only shows it masked.
func (h *Handler) HandleAdminAPIToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newToken string
	switch r.FormValue("action") {
	case "generate":
		tok, err := generateAPIToken()
		if err != nil {
			log.Printf("Error generating API token: %v", err)
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}
		newToken = tok
	case "clear":
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	if err := h.db.SetSetting("api_token", newToken); err != nil {
		log.Printf("Error saving api_token setting: %v", err)
		http.Error(w, "Failed to save API token", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/admin?tab=settings", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if newToken == "" {
		fmt.Fprint(w, `<span class="alert alert-success" style="display: inline-flex; padding: 0.4rem 0.85rem; margin: 0; font-size: 0.875rem; border-radius: 6px;">✓ API token cleared. The stored token no longer grants access.</span>`)
		return
	}
	fmt.Fprintf(w, `<div class="alert alert-success" style="display: flex; flex-direction: column; gap: 0.5rem; padding: 0.75rem 1rem; margin: 0; font-size: 0.875rem; border-radius: 6px;">
		<span>✓ New API token generated. Copy it now; it will only be shown masked after you leave this page.</span>
		<code id="api-token-new" style="user-select: all; word-break: break-all; font-size: 0.95rem;">%s</code>
	</div>`, template.HTMLEscapeString(newToken))
}
