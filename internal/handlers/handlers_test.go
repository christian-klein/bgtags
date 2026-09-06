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

	// 1. Test Index
	wIndex := httptest.NewRecorder()
	rIndex := httptest.NewRequest("GET", "/", nil)
	h.HandleIndex(wIndex, rIndex)
	if wIndex.Code != http.StatusOK {
		t.Errorf("expected 200 for index, got %d", wIndex.Code)
	}
	if !strings.Contains(wIndex.Body.String(), "Board Game Rules & QR Tags") {
		t.Errorf("index page missing hero title")
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
		BaseURL:   "https://bgtags.cklein.us",
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
	if !strings.Contains(body, "Eclipse: Second Dawn") {
		t.Errorf("expected game title in body")
	}
	if !strings.Contains(body, "Species Glossary") {
		t.Errorf("expected doc title in body")
	}
	if !strings.Contains(body, "bgtags.cklein.us") {
		t.Errorf("expected configured BaseURL in body/QR")
	}
}
