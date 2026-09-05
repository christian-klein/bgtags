package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
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
