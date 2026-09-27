package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/christian-klein/bgtags/internal/database"
)

const testAPIToken = "test-api-token-0123456789"

func setupAPITestHandler(t *testing.T) (*Handler, *database.DB, map[string]*database.Game) {
	t.Helper()
	h, db, _ := setupTestHandler(t)
	t.Cleanup(func() { db.Close() })
	h.cfg.BaseURL = "https://bgtags.example.com/"

	bgg := func(n int) *int { return &n }
	games := map[string]*database.Game{
		"azul":     {Name: "Azul", BggID: bgg(230802), Image: "azul cover.png", MinPlayers: 2, MaxPlayers: 4, BestPlayers: "2", Complexity: 1.76, Rating: 7.7, BggURL: "https://boardgamegeek.com/boardgame/230802/azul"},
		"brass":    {Name: "Brass: Birmingham", BggID: bgg(224517), Image: "brass.jpg", MinPlayers: 2, MaxPlayers: 4, BestPlayers: "3-4", Complexity: 3.87, Rating: 8.6},
		"catan":    {Name: "Catan", MinPlayers: 3, MaxPlayers: 4, BestPlayers: "4", Complexity: 2.3},
		"seafarer": {Name: "Catan: Seafarers", BggID: bgg(325), MinPlayers: 3, MaxPlayers: 4, Complexity: 2.4},
		"percent":  {Name: "100% Orange Juice", MinPlayers: 2, MaxPlayers: 4},
	}
	for _, key := range []string{"azul", "brass", "catan", "percent"} {
		if err := db.CreateGame(games[key]); err != nil {
			t.Fatalf("create %s: %v", key, err)
		}
	}
	parent := games["catan"].ID
	games["seafarer"].ParentID = &parent
	if err := db.CreateGame(games["seafarer"]); err != nil {
		t.Fatalf("create expansion: %v", err)
	}

	for _, key := range []string{"catan", "seafarer", "azul"} {
		if err := db.AddGameToUserCollection("alice", games[key].ID); err != nil {
			t.Fatalf("add to alice: %v", err)
		}
	}
	if err := db.AddGameToUserCollection("bob", games["brass"].ID); err != nil {
		t.Fatalf("add to bob: %v", err)
	}
	if err := db.SetCollectionName("alice", "Alice's Shelf"); err != nil {
		t.Fatalf("set collection name: %v", err)
	}
	return h, db, games
}

func apiRequest(t *testing.T, h *Handler, method, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.HandleAPI(rec, req)
	return rec
}

func decodeAPIGames(t *testing.T, rec *httptest.ResponseRecorder) []APIGame {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Games []APIGame `json:"games"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if resp.Games == nil {
		t.Fatalf("expected games array, got null: %s", rec.Body.String())
	}
	return resp.Games
}

func gameNames(games []APIGame) string {
	names := make([]string, len(games))
	for i, g := range games {
		names[i] = g.Name
	}
	return strings.Join(names, "|")
}

func TestAPIDisabledWithoutToken(t *testing.T) {
	h, _, _ := setupAPITestHandler(t)
	rec := apiRequest(t, h, "GET", "/api/v1/collections", "anything")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no token configured, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestAPIAuth(t *testing.T) {
	h, db, _ := setupAPITestHandler(t)
	if err := db.SetSetting("api_token", testAPIToken); err != nil {
		t.Fatal(err)
	}

	for _, tok := range []string{"", "wrong-token", testAPIToken + "x"} {
		rec := apiRequest(t, h, "GET", "/api/v1/collections", tok)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q: expected 401, got %d", tok, rec.Code)
		}
		if strings.TrimSpace(rec.Body.String()) != `{"error":"unauthorized"}` {
			t.Errorf("unexpected 401 body: %s", rec.Body.String())
		}
	}

	// Non-Bearer scheme is rejected
	req := httptest.NewRequest("GET", "/api/v1/collections", nil)
	req.Header.Set("Authorization", "Basic "+testAPIToken)
	rec := httptest.NewRecorder()
	h.HandleAPI(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for Basic scheme, got %d", rec.Code)
	}

	// Only GET is allowed
	if rec := apiRequest(t, h, "POST", "/api/v1/games", testAPIToken); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}

	// Unknown endpoint
	if rec := apiRequest(t, h, "GET", "/api/v1/nope", testAPIToken); rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown endpoint, got %d", rec.Code)
	}

	// Env token overrides the stored setting
	h.cfg.APIToken = "env-token"
	if rec := apiRequest(t, h, "GET", "/api/v1/collections", testAPIToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("expected stored token to be ignored when API_TOKEN is set, got %d", rec.Code)
	}
	if rec := apiRequest(t, h, "GET", "/api/v1/collections", "env-token"); rec.Code != http.StatusOK {
		t.Errorf("expected env token to be accepted, got %d", rec.Code)
	}
}

func TestAPICollections(t *testing.T) {
	h, _, _ := setupAPITestHandler(t)
	h.cfg.APIToken = testAPIToken

	rec := apiRequest(t, h, "GET", "/api/v1/collections", testAPIToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control no-store, got %q", cc)
	}
	want := `{"collections":[{"id":"alice","name":"Alice's Shelf"},{"id":"bob","name":"bob"}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("unexpected body:\n got %s\nwant %s", got, want)
	}
}

func TestAPIGamesSearch(t *testing.T) {
	h, _, games := setupAPITestHandler(t)
	h.cfg.APIToken = testAPIToken

	// All games (base + expansions), sorted by name
	all := decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games", testAPIToken))
	if got := gameNames(all); got != "100% Orange Juice|Azul|Brass: Birmingham|Catan|Catan: Seafarers" {
		t.Errorf("unexpected all-games order: %s", got)
	}

	// Case-insensitive name contains
	res := decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games?q=CATAN", testAPIToken))
	if got := gameNames(res); got != "Catan|Catan: Seafarers" {
		t.Errorf("unexpected q=CATAN result: %s", got)
	}

	// LIKE wildcards are matched literally
	res = decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games?q=%25", testAPIToken))
	if got := gameNames(res); got != "100% Orange Juice" {
		t.Errorf("unexpected q=%% result: %s", got)
	}

	// Collection filter
	res = decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games?collection=alice", testAPIToken))
	if got := gameNames(res); got != "Azul|Catan|Catan: Seafarers" {
		t.Errorf("unexpected alice collection: %s", got)
	}
	res = decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games?q=sea&collection=alice", testAPIToken))
	if len(res) != 1 || res[0].ID != games["seafarer"].ID {
		t.Errorf("unexpected q+collection result: %s", gameNames(res))
	}
	res = decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games?q=azul&collection=bob", testAPIToken))
	if len(res) != 0 {
		t.Errorf("expected no results, got %s", gameNames(res))
	}

	// Limit
	res = decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games?limit=2", testAPIToken))
	if got := gameNames(res); got != "100% Orange Juice|Azul" {
		t.Errorf("unexpected limited result: %s", got)
	}
	if rec := apiRequest(t, h, "GET", "/api/v1/games?limit=abc", testAPIToken); rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid limit, got %d", rec.Code)
	}
}

func TestAPIGameJSONAndAbsoluteURLs(t *testing.T) {
	h, _, games := setupAPITestHandler(t)
	h.cfg.APIToken = testAPIToken

	rec := apiRequest(t, h, "GET", "/api/v1/games?q=azul", testAPIToken)
	res := decodeAPIGames(t, rec)
	if len(res) != 1 {
		t.Fatalf("expected 1 game, got %d", len(res))
	}
	g := res[0]
	azul := games["azul"]
	rulesURL := "https://bgtags.example.com/games/" + itoa(azul.ID) + "/rules"
	if g.RulesURL != rulesURL {
		t.Errorf("rules_url = %q, want %q", g.RulesURL, rulesURL)
	}
	if want := "https://bgtags.example.com/qr?url=" + url.QueryEscape(rulesURL); g.QRURL != want {
		t.Errorf("qr_url = %q, want %q", g.QRURL, want)
	}
	if want := "https://bgtags.example.com/img/azul%20cover.png"; g.ImageURL != want {
		t.Errorf("image_url = %q, want %q", g.ImageURL, want)
	}
	if g.BggID == nil || *g.BggID != 230802 || g.MinPlayers != 2 || g.MaxPlayers != 4 || g.BestPlayers != "2" || g.Complexity != 1.76 || g.Rating != 7.7 || g.BggURL != azul.BggURL {
		t.Errorf("unexpected game fields: %+v", g)
	}

	// Exact key set, bgg_id null and image_url "" when absent
	var raw struct {
		Games []map[string]interface{} `json:"games"`
	}
	rec = apiRequest(t, h, "GET", "/api/v1/games?q=orange", testAPIToken)
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw.Games) != 1 {
		t.Fatalf("decode raw: %v %s", err, rec.Body.String())
	}
	obj := raw.Games[0]
	keys := []string{"id", "bgg_id", "name", "image_url", "min_players", "max_players", "best_players", "complexity", "rating", "bgg_url", "rules_url", "qr_url"}
	if len(obj) != len(keys) {
		t.Errorf("expected %d keys, got %d: %v", len(keys), len(obj), obj)
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if obj["bgg_id"] != nil {
		t.Errorf("expected bgg_id null, got %v", obj["bgg_id"])
	}
	if obj["image_url"] != "" {
		t.Errorf("expected empty image_url, got %v", obj["image_url"])
	}
}

func TestAPILookup(t *testing.T) {
	h, _, games := setupAPITestHandler(t)
	h.cfg.APIToken = testAPIToken

	res := decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games/lookup?bgg_id=224517,325,999999", testAPIToken))
	if got := gameNames(res); got != "Brass: Birmingham|Catan: Seafarers" {
		t.Errorf("unexpected bgg_id lookup: %s", got)
	}

	target := "/api/v1/games/lookup?id=" + itoa(games["catan"].ID) + "&bgg_id=230802"
	res = decodeAPIGames(t, apiRequest(t, h, "GET", target, testAPIToken))
	if got := gameNames(res); got != "Azul|Catan" {
		t.Errorf("unexpected mixed lookup: %s", got)
	}

	// Non-numeric ids are ignored
	res = decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games/lookup?bgg_id=abc,230802&id=x", testAPIToken))
	if got := gameNames(res); got != "Azul" {
		t.Errorf("unexpected lookup with junk ids: %s", got)
	}

	// No valid ids -> 400
	for _, q := range []string{"", "?bgg_id=abc", "?id=,,x&bgg_id=-1"} {
		rec := apiRequest(t, h, "GET", "/api/v1/games/lookup"+q, testAPIToken)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("lookup%s: expected 400, got %d", q, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("expected JSON error body, got %s", rec.Body.String())
		}
	}

	// More than 200 ids are capped rather than rejected
	ids := make([]string, 250)
	for i := range ids {
		ids[i] = itoa(int64(i + 1))
	}
	res = decodeAPIGames(t, apiRequest(t, h, "GET", "/api/v1/games/lookup?id="+strings.Join(ids, ","), testAPIToken))
	if len(res) != 5 {
		t.Errorf("expected 5 games from capped lookup, got %d", len(res))
	}
}

func TestAdminAPITokenGenerate(t *testing.T) {
	h, db, _ := setupAPITestHandler(t)

	req := httptest.NewRequest("POST", "/admin/settings/api-token", strings.NewReader("action=generate"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.HandleAdminAPIToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	tok, _ := db.GetSetting("api_token", "")
	if len(tok) != 43 {
		t.Fatalf("expected 43-char urlsafe token, got %q", tok)
	}
	if !strings.Contains(rec.Body.String(), tok) {
		t.Errorf("expected new token shown in full in response")
	}

	// Admin page only shows the masked token
	recAdmin := httptest.NewRecorder()
	h.HandleAdmin(recAdmin, httptest.NewRequest("GET", "/admin", nil))
	body := recAdmin.Body.String()
	if strings.Contains(body, tok) {
		t.Errorf("admin page must not render the full API token")
	}
	if !strings.Contains(body, tok[len(tok)-4:]) || !strings.Contains(body, "JSON API Access Token") {
		t.Errorf("expected masked token (last 4 chars) in admin page")
	}

	// Generated token works against the API
	if rec := apiRequest(t, h, "GET", "/api/v1/collections", tok); rec.Code != http.StatusOK {
		t.Errorf("expected generated token to authorize API, got %d", rec.Code)
	}

	// Clear disables the API
	req = httptest.NewRequest("POST", "/admin/settings/api-token", strings.NewReader("action=clear"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.HandleAdminAPIToken(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 for non-HTMX clear, got %d", rec.Code)
	}
	if rec := apiRequest(t, h, "GET", "/api/v1/collections", tok); rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after clearing token, got %d", rec.Code)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
