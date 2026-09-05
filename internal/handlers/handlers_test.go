package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
