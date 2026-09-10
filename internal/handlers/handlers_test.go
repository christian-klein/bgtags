package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"fmt"
	"strings"

	"github.com/christian-klein/bgtags/internal/config"
	"github.com/christian-klein/bgtags/internal/database"
)

func TestQRAndHealthHandlers(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:      "8081",
		BackupDir: filepath.Join(tempDir, "backups"),
	}

	h := &Handler{
		db:  db,
		cfg: cfg,
	}

	// Test Health Handler
	wHealth := httptest.NewRecorder()
	rHealth := httptest.NewRequest("GET", "/health", nil)
	h.HandleHealth(wHealth, rHealth)

	if wHealth.Code != http.StatusOK {
		t.Errorf("expected status 200 for /health, got %d", wHealth.Code)
	}
	if wHealth.Body.String() != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", wHealth.Body.String())
	}

	// Test QR Handler
	wQR := httptest.NewRecorder()
	rQR := httptest.NewRequest("GET", "/qr?url=https://example.com/rules.pdf", nil)
	h.HandleQR(wQR, rQR)

	if wQR.Code != http.StatusOK {
		t.Errorf("expected status 200 for /qr, got %d", wQR.Code)
	}
	if wQR.Header().Get("Content-Type") != "image/png" {
		t.Errorf("expected Content-Type image/png, got %s", wQR.Header().Get("Content-Type"))
	}
	if wQR.Body.Len() == 0 {
		t.Errorf("expected non-empty QR code PNG body")
	}
}

func TestPagesRender(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:      "8081",
		BackupDir: filepath.Join(tempDir, "backups"),
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// 1. Test Index (Empty DB)
	wIndex := httptest.NewRecorder()
	rIndex := httptest.NewRequest("GET", "/", nil)
	h.HandleIndex(wIndex, rIndex)
	if wIndex.Code != http.StatusOK {
		t.Errorf("expected 200 for index, got %d", wIndex.Code)
	}
	if !strings.Contains(wIndex.Body.String(), "Board Game Rules & QR Tags") {
		t.Errorf("index page missing hero title")
	}

	// Test Index with games and expansions
	baseGame := &database.Game{Name: "Core Game", URL: "core.pdf"}
	if err := db.CreateGame(baseGame); err != nil {
		t.Fatalf("failed to create base game: %v", err)
	}
	parentID := baseGame.ID
	expansion := &database.Game{Name: "Exp 1", URL: "exp.pdf", ParentID: &parentID}
	if err := db.CreateGame(expansion); err != nil {
		t.Fatalf("failed to create expansion: %v", err)
	}

	wIndex2 := httptest.NewRecorder()
	rIndex2 := httptest.NewRequest("GET", "/", nil)
	h.HandleIndex(wIndex2, rIndex2)
	if wIndex2.Code != http.StatusOK {
		t.Errorf("expected 200 for index, got %d", wIndex2.Code)
	}
	body2 := wIndex2.Body.String()
	if !strings.Contains(body2, "Currently featuring") || !strings.Contains(body2, "1</span> game") || !strings.Contains(body2, "1</span> expansion") {
		t.Errorf("index page missing collection stats, body: %s", body2)
	}

	// 2. Test Stickers
	wStickers := httptest.NewRecorder()
	rStickers := httptest.NewRequest("GET", "/stickers", nil)
	h.HandleStickers(wStickers, rStickers)
	if wStickers.Code != http.StatusOK {
		t.Errorf("expected 200 for stickers, got %d", wStickers.Code)
	}
	if !strings.Contains(wStickers.Body.String(), "Game Box Stickers") {
		t.Errorf("stickers page missing title")
	}

	// 3. Test Games partial
	wGames := httptest.NewRecorder()
	rGames := httptest.NewRequest("GET", "/games", nil)
	h.HandleGames(wGames, rGames)
	if wGames.Code != http.StatusOK {
		t.Errorf("expected 200 for games partial, got %d", wGames.Code)
	}

	// 4. Test Games partial with complexity filtering
	wComp := httptest.NewRecorder()
	rComp := httptest.NewRequest("GET", "/games?min_complexity=2.0&max_complexity=3.5", nil)
	h.HandleGames(wComp, rComp)
	if wComp.Code != http.StatusOK {
		t.Errorf("expected 200 for games partial with complexity, got %d", wComp.Code)
	}
}

func TestBaseURLResolution(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// 1. Explicit BaseURL set
	hStatic := &Handler{
		db: db,
		cfg: &config.Config{
			BaseURL: "https://bgtags.example.com",
		},
	}
	r1 := httptest.NewRequest("GET", "http://localhost:8082/", nil)
	if url := hStatic.getBaseURL(r1); url != "https://bgtags.example.com" {
		t.Errorf("expected static BASE_URL https://bgtags.example.com, got %s", url)
	}

	// Case 2: No BASE_URL set, should inspect X-Forwarded headers or Host
	hDynamic := &Handler{
		db: db,
		cfg: &config.Config{
			BaseURL: "",
		},
	}

	rProxy := httptest.NewRequest("GET", "http://localhost:8080/games/1", nil)
	rProxy.Header.Set("X-Forwarded-Proto", "https")
	rProxy.Header.Set("X-Forwarded-Host", "bgtags.custom-domain.org")
	if url := hDynamic.getBaseURL(rProxy); url != "https://bgtags.custom-domain.org" {
		t.Errorf("expected https://bgtags.custom-domain.org from headers, got %s", url)
	}

	rDirect := httptest.NewRequest("GET", "http://localhost:8082/games/1", nil)
	if url := hDynamic.getBaseURL(rDirect); url != "http://localhost:8082" {
		t.Errorf("expected http://localhost:8082 from request host, got %s", url)
	}
}

func TestRulesHubRender(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "rules_test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	game := &database.Game{
		Name:        "Eclipse: Second Dawn",
		URL:         "eclipse-rules.pdf",
		Image:       "eclipse.webp",
		MinPlayers:  2,
		MaxPlayers:  6,
		BestPlayers: "4-6",
		Complexity:  3.6,
	}
	if err := db.CreateGame(game); err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	doc := &database.GameDocument{
		GameID:    game.ID,
		Title:     "Species Glossary",
		Category:  "glossary",
		Filename:  "eclipse-glossary.pdf",
		IsPrimary: false,
	}
	if err := db.AddDocument(doc); err != nil {
		t.Fatalf("failed to add doc: %v", err)
	}

	cfg := &config.Config{
		BaseURL:   "https://bgtags.example.com",
		BackupDir: filepath.Join(tempDir, "backups"),
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", fmt.Sprintf("/games/%d/rules", game.ID), nil)
	h.HandleGameRoute(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for rules hub, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Eclipse: Second Dawn - Rules &amp; Docs") {
		t.Errorf("expected page title 'Eclipse: Second Dawn - Rules &amp; Docs', got: %s", body)
	}
	if !strings.Contains(body, "📚 Rules &amp; Docs") && !strings.Contains(body, "📚 Rules & Docs") {
		t.Errorf("expected section title '📚 Rules & Docs' in body")
	}
	if !strings.Contains(body, fmt.Sprintf("href=\"/stickers?game_id=%d\"", game.ID)) {
		t.Errorf("expected single-game sticker link '/stickers?game_id=%d' in body", game.ID)
	}
	if !strings.Contains(body, "Eclipse: Second Dawn") {
		t.Errorf("expected game title in body")
	}
	if !strings.Contains(body, "Species Glossary") {
		t.Errorf("expected doc title in body")
	}
	if !strings.Contains(body, "bgtags.example.com") {
		t.Errorf("expected configured BaseURL in body/QR")
	}
	if !strings.Contains(body, "← Back to Catalog") {
		t.Errorf("expected base game to have '← Back to Catalog'")
	}

	// 2. Test Expansion Game rules hub links back to parent game
	exp := &database.Game{
		ParentID:   &game.ID,
		Name:       "Eclipse: Second Dawn - Worlds Afar",
		URL:        "eclipse-worlds-afar.pdf",
		Image:      "eclipse-exp.webp",
		MinPlayers: 2,
		MaxPlayers: 6,
	}
	if err := db.CreateGame(exp); err != nil {
		t.Fatalf("failed to create expansion: %v", err)
	}

	wExp := httptest.NewRecorder()
	rExp := httptest.NewRequest("GET", fmt.Sprintf("/games/%d/rules", exp.ID), nil)
	h.HandleGameRoute(wExp, rExp)

	if wExp.Code != http.StatusOK {
		t.Fatalf("expected 200 for expansion rules hub, got %d", wExp.Code)
	}
	expBody := wExp.Body.String()
	expectedBackLink := fmt.Sprintf("href=\"/games/%d/rules\"", game.ID)
	if !strings.Contains(expBody, expectedBackLink) {
		t.Errorf("expected expansion rules hub to link back to parent game: %s", expectedBackLink)
	}
	if !strings.Contains(expBody, "← Back to Eclipse: Second Dawn") {
		t.Errorf("expected expansion rules hub to display '← Back to Eclipse: Second Dawn'")
	}

	// 3. Test base game rules hub: Available Expansions list with setting OFF (default)
	wBase := httptest.NewRecorder()
	rBase := httptest.NewRequest("GET", fmt.Sprintf("/games/%d/rules", game.ID), nil)
	h.HandleGameRoute(wBase, rBase)
	if !strings.Contains(wBase.Body.String(), "Eclipse: Second Dawn - Worlds Afar") {
		t.Errorf("expected full expansion name 'Eclipse: Second Dawn - Worlds Afar' when setting is OFF")
	}

	// 4. Enable hide_game_title_in_expansions setting
	if err := db.SetSettingBool("hide_game_title_in_expansions", true); err != nil {
		t.Fatalf("failed to enable setting: %v", err)
	}

	// 5. Test base game rules hub: Available Expansions list with setting ON
	wBaseClean := httptest.NewRecorder()
	rBaseClean := httptest.NewRequest("GET", fmt.Sprintf("/games/%d/rules", game.ID), nil)
	h.HandleGameRoute(wBaseClean, rBaseClean)
	bodyClean := wBaseClean.Body.String()
	if !strings.Contains(bodyClean, "Worlds Afar") {
		t.Errorf("expected stripped expansion name 'Worlds Afar' when setting is ON")
	}
	// Verify "Eclipse: Worlds Afar" is NOT in the expansion title header
	if strings.Contains(bodyClean, "<h4 class=\"exp-title\">Eclipse: Worlds Afar</h4>") {
		t.Errorf("expected parent title to be stripped from expansion title header")
	}
}

func TestPWAEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:      "8081",
		StaticDir: "../../static",
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Test Manifest
	wManifest := httptest.NewRecorder()
	rManifest := httptest.NewRequest("GET", "/manifest.webmanifest", nil)
	h.HandleManifest(wManifest, rManifest)

	if wManifest.Code != http.StatusOK {
		t.Errorf("expected status 200 for /manifest.webmanifest, got %d", wManifest.Code)
	}
	if !strings.Contains(wManifest.Header().Get("Content-Type"), "application/manifest+json") {
		t.Errorf("expected Content-Type application/manifest+json, got %s", wManifest.Header().Get("Content-Type"))
	}
	if !strings.Contains(wManifest.Body.String(), "BG Tags") {
		t.Errorf("expected manifest body to contain BG Tags")
	}

	// Test Service Worker
	wSW := httptest.NewRecorder()
	rSW := httptest.NewRequest("GET", "/sw.js", nil)
	h.HandleServiceWorker(wSW, rSW)

	if wSW.Code != http.StatusOK {
		t.Errorf("expected status 200 for /sw.js, got %d", wSW.Code)
	}
	if !strings.Contains(wSW.Header().Get("Content-Type"), "application/javascript") {
		t.Errorf("expected Content-Type application/javascript, got %s", wSW.Header().Get("Content-Type"))
	}
	if wSW.Header().Get("Service-Worker-Allowed") != "/" {
		t.Errorf("expected Service-Worker-Allowed header to be '/', got %s", wSW.Header().Get("Service-Worker-Allowed"))
	}
	if !strings.Contains(wSW.Body.String(), "bgtags-v2") {
		t.Errorf("expected service worker body to contain cache version bgtags-v2")
	}
}

func TestRatingAndQRButtonRendering(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:      "8081",
		StaticDir: "../../static",
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	game := &database.Game{
		Name:       "Ark Nova",
		URL:        "ark-nova.pdf",
		Image:      "ark-nova.jpg",
		MinPlayers: 1,
		MaxPlayers: 4,
		Rating:     8.53,
		Complexity: 3.74,
	}
	if err := db.CreateGame(game); err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	// 1. Check Catalog Rendering
	wGames := httptest.NewRecorder()
	rGames := httptest.NewRequest("GET", "/games", nil)
	h.HandleGames(wGames, rGames)

	if wGames.Code != http.StatusOK {
		t.Fatalf("expected 200 for /games, got %d", wGames.Code)
	}
	gamesBody := wGames.Body.String()
	if !strings.Contains(gamesBody, "card-qr-btn") {
		t.Errorf("expected dedicated QR code button on game card")
	}
	if !strings.Contains(gamesBody, "⭐ 8.5") {
		t.Errorf("expected rating badge ⭐ 8.5 on game card, got: %s", gamesBody)
	}
	if !strings.Contains(gamesBody, "title=\"BGG Complexity: 3.74 / 5\"") {
		t.Errorf("expected complexity tooltip on game card, got: %s", gamesBody)
	}
	if !strings.Contains(gamesBody, fmt.Sprintf("/games/%d/rules", game.ID)) {
		t.Errorf("expected game cover image link to point to rules hub")
	}

	// 2. Check Rules Hub Rendering
	// Add secondary doc so it renders hub page rather than 302
	doc := &database.GameDocument{
		GameID:    game.ID,
		Title:     "Reference Guide",
		Category:  "reference",
		Filename:  "ark-nova-ref.pdf",
		IsPrimary: false,
	}
	if err := db.AddDocument(doc); err != nil {
		t.Fatalf("failed to add document: %v", err)
	}

	wHub := httptest.NewRecorder()
	rHub := httptest.NewRequest("GET", fmt.Sprintf("/games/%d/rules", game.ID), nil)
	h.HandleGameRoute(wHub, rHub)

	if wHub.Code != http.StatusOK {
		t.Fatalf("expected 200 for rules hub, got %d", wHub.Code)
	}
	hubBody := wHub.Body.String()
	if !strings.Contains(hubBody, "⭐ Rating: 8.5 / 10") {
		t.Errorf("expected rating badge in rules hub, got: %s", hubBody)
	}
	if !strings.Contains(hubBody, "title=\"BGG Complexity: 3.74 / 5\"") {
		t.Errorf("expected complexity tooltip in rules hub, got: %s", hubBody)
	}
}

func TestSortMenuAndFiltering(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_sort_ui.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:      "8081",
		StaticDir: "../../static",
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create test games
	games := []*database.Game{
		{Name: "Agricola", URL: "agricola.pdf", Rating: 7.9, Complexity: 3.6, MinPlayers: 1, MaxPlayers: 4},
		{Name: "Blood Rage", URL: "bloodrage.pdf", Rating: 8.0, Complexity: 2.8, MinPlayers: 2, MaxPlayers: 4},
		{Name: "Codenames", URL: "codenames.pdf", Rating: 7.5, Complexity: 1.2, MinPlayers: 2, MaxPlayers: 8},
	}
	for _, g := range games {
		if err := db.CreateGame(g); err != nil {
			t.Fatalf("failed to create game: %v", err)
		}
	}

	// 1. Verify Index page has sort button and menu, and stickers link is removed from filters bar
	wIndex := httptest.NewRecorder()
	rIndex := httptest.NewRequest("GET", "/", nil)
	h.HandleIndex(wIndex, rIndex)

	if wIndex.Code != http.StatusOK {
		t.Fatalf("expected 200 for index, got %d", wIndex.Code)
	}
	indexBody := wIndex.Body.String()
	if !strings.Contains(indexBody, "id=\"sort-menu-btn\"") {
		t.Errorf("expected sort-menu-btn in index page")
	}
	if !strings.Contains(indexBody, "id=\"sort-dropdown-menu\"") {
		t.Errorf("expected sort-dropdown-menu in index page")
	}
	if !strings.Contains(indexBody, "name=\"sort_by\"") || !strings.Contains(indexBody, "name=\"sort_order\"") {
		t.Errorf("expected hidden sort inputs in index page")
	}
	if strings.Contains(indexBody, "<a href=\"/stickers\" class=\"btn btn-secondary\">") {
		t.Errorf("stickers button should be removed from the catalog filter bar")
	}

	// 2. Test /games?sort_by=rating&sort_order=desc
	wRatingDesc := httptest.NewRecorder()
	rRatingDesc := httptest.NewRequest("GET", "/games?sort_by=rating&sort_order=desc", nil)
	h.HandleGames(wRatingDesc, rRatingDesc)

	bodyRating := wRatingDesc.Body.String()
	idxBloodRage := strings.Index(bodyRating, "Blood Rage") // 8.0
	idxAgricola := strings.Index(bodyRating, "Agricola")   // 7.9
	idxCodenames := strings.Index(bodyRating, "Codenames") // 7.5

	if idxBloodRage == -1 || idxAgricola == -1 || idxCodenames == -1 {
		t.Fatalf("missing expected games in rating sorted grid")
	}
	if !(idxBloodRage < idxAgricola && idxAgricola < idxCodenames) {
		t.Errorf("expected rating desc order: Blood Rage (8.0), Agricola (7.9), Codenames (7.5)")
	}

	// 3. Test /games?sort_by=complexity&sort_order=asc
	wCompAsc := httptest.NewRecorder()
	rCompAsc := httptest.NewRequest("GET", "/games?sort_by=complexity&sort_order=asc", nil)
	h.HandleGames(wCompAsc, rCompAsc)

	bodyComp := wCompAsc.Body.String()
	idxCodenamesComp := strings.Index(bodyComp, "Codenames") // 1.2
	idxBloodRageComp := strings.Index(bodyComp, "Blood Rage") // 2.8
	idxAgricolaComp := strings.Index(bodyComp, "Agricola")   // 3.6

	if !(idxCodenamesComp < idxBloodRageComp && idxBloodRageComp < idxAgricolaComp) {
		t.Errorf("expected complexity asc order: Codenames (1.2), Blood Rage (2.8), Agricola (3.6)")
	}

	// 4. Test /games?sort_by=max_players&sort_order=desc
	wMaxDesc := httptest.NewRecorder()
	rMaxDesc := httptest.NewRequest("GET", "/games?sort_by=max_players&sort_order=desc", nil)
	h.HandleGames(wMaxDesc, rMaxDesc)

	bodyMax := wMaxDesc.Body.String()
	idxCodenamesMax := strings.Index(bodyMax, "Codenames") // 8 players
	idxAgricolaMax := strings.Index(bodyMax, "Agricola")   // 4 players

	if !(idxCodenamesMax < idxAgricolaMax) {
		t.Errorf("expected Codenames (8 players) to come before Agricola (4 players) in max_players desc")
	}
}

func TestStickersSingleGameFilter(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_stickers.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:      "8081",
		StaticDir: "../../static",
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	g1 := &database.Game{Name: "Gloomhaven", URL: "gh.pdf", Rating: 8.7, Complexity: 3.9, MinPlayers: 1, MaxPlayers: 4}
	g2 := &database.Game{Name: "Spirit Island", URL: "si.pdf", Rating: 8.3, Complexity: 4.1, MinPlayers: 1, MaxPlayers: 4}
	if err := db.CreateGame(g1); err != nil {
		t.Fatalf("failed to create g1: %v", err)
	}
	if err := db.CreateGame(g2); err != nil {
		t.Fatalf("failed to create g2: %v", err)
	}

	// 1. Request all stickers
	wAll := httptest.NewRecorder()
	rAll := httptest.NewRequest("GET", "/stickers", nil)
	h.HandleStickers(wAll, rAll)
	if wAll.Code != http.StatusOK {
		t.Fatalf("expected 200 for all stickers, got %d", wAll.Code)
	}
	bodyAll := wAll.Body.String()
	if !strings.Contains(bodyAll, "Gloomhaven") || !strings.Contains(bodyAll, "Spirit Island") {
		t.Errorf("expected both games in all stickers view")
	}
	if !strings.Contains(bodyAll, "Back to Catalog") {
		t.Errorf("expected 'Back to Catalog' link when displaying multiple stickers")
	}

	// 2. Request single game sticker with ?game_id=...
	wSingle := httptest.NewRecorder()
	rSingle := httptest.NewRequest("GET", fmt.Sprintf("/stickers?game_id=%d", g1.ID), nil)
	h.HandleStickers(wSingle, rSingle)
	if wSingle.Code != http.StatusOK {
		t.Fatalf("expected 200 for single sticker, got %d", wSingle.Code)
	}
	bodySingle := wSingle.Body.String()
	if !strings.Contains(bodySingle, "Gloomhaven") {
		t.Errorf("expected Gloomhaven sticker")
	}
	if strings.Contains(bodySingle, "Spirit Island") {
		t.Errorf("did not expect Spirit Island in filtered single sticker view")
	}
	if !strings.Contains(bodySingle, fmt.Sprintf("/games/%d/rules", g1.ID)) {
		t.Errorf("expected link back to game rules hub in single sticker view")
	}
	if !strings.Contains(bodySingle, "All Stickers") {
		t.Errorf("expected 'All Stickers' link in single sticker view")
	}
}

func TestBackToTopButton(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_btt.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:      "8081",
		StaticDir: "../../static",
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.HandleIndex(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for index, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "id=\"back-to-top-btn\"") {
		t.Errorf("expected back-to-top button in layout")
	}
	if !strings.Contains(body, "back-to-top-icon") {
		t.Errorf("expected back-to-top-icon in layout")
	}
}
