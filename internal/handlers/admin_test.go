package handlers

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christian-klein/bgtags/internal/config"
	"github.com/christian-klein/bgtags/internal/database"
)

func setupTestHandler(t *testing.T) (*Handler, *database.DB, string) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "admin_test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	cfg := &config.Config{
		Port:        "8081",
		StaticDir:   tempDir,
		BackupDir:   filepath.Join(tempDir, "backups"),
		OIDCEnabled: false,
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	return h, db, tempDir
}

func TestAdminPageAndGameCRUD(t *testing.T) {
	h, db, _ := setupTestHandler(t)
	defer db.Close()

	// 1. Initial Admin Page Render
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin", nil)
	h.HandleAdmin(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /admin, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Admin Control Panel") {
		t.Errorf("expected admin control panel in body")
	}

	// 2. Create Game via Multipart Form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("name", "Terraforming Mars")
	_ = writer.WriteField("min_players", "1")
	_ = writer.WriteField("max_players", "5")
	_ = writer.WriteField("best_players", "3-4")
	_ = writer.WriteField("complexity", "3.25")
	_ = writer.WriteField("bgg_url", "https://boardgamegeek.com/boardgame/167791/terraforming-mars")
	_ = writer.WriteField("pdf_name", "tm-rules.pdf")
	_ = writer.WriteField("image_name", "tm.jpg")
	_ = writer.Close()

	recCreate := httptest.NewRecorder()
	reqCreate := httptest.NewRequest("POST", "/admin/games/create", &buf)
	reqCreate.Header.Set("Content-Type", writer.FormDataContentType())
	h.HandleAdminCreateGame(recCreate, reqCreate)

	if recCreate.Code != http.StatusOK {
		t.Fatalf("expected 200 for game create, got %d", recCreate.Code)
	}
	if !strings.Contains(recCreate.Body.String(), "Terraforming Mars") {
		t.Errorf("expected Terraforming Mars in returned game table")
	}

	// Verify in DB
	games, err := db.ListGames("Terraforming Mars", 0)
	if err != nil || len(games) == 0 {
		t.Fatalf("game not found in database")
	}
	gameID := games[0].ID

	// 3. Add Document to Game
	var docBuf bytes.Buffer
	docWriter := multipart.NewWriter(&docBuf)
	_ = docWriter.WriteField("title", "Corporation Reference")
	_ = docWriter.WriteField("category", "reference")
	_ = docWriter.WriteField("pdf_name", "tm-corps.pdf")
	_ = docWriter.WriteField("is_primary", "false")
	_ = docWriter.Close()

	recDoc := httptest.NewRecorder()
	reqDoc := httptest.NewRequest("POST", "/admin/games/1/documents", &docBuf)
	reqDoc.Header.Set("Content-Type", docWriter.FormDataContentType())
	h.handleAdminAddDocument(recDoc, reqDoc, gameID)

	if recDoc.Code != http.StatusOK {
		t.Fatalf("expected 200 for doc create, got %d", recDoc.Code)
	}
	if !strings.Contains(recDoc.Body.String(), "Corporation Reference") {
		t.Errorf("expected doc title in returned table")
	}

	docs, err := db.ListDocuments(gameID)
	if err != nil || len(docs) == 0 {
		t.Fatalf("expected documents for game")
	}

	// 4. Delete Document
	var docToDelete int64
	for _, d := range docs {
		if d.Title == "Corporation Reference" {
			docToDelete = d.ID
			break
		}
	}
	if docToDelete == 0 {
		t.Fatalf("could not find added document")
	}

	recDelDoc := httptest.NewRecorder()
	h.HandleAdminDocumentRoute(recDelDoc, httptest.NewRequest("DELETE", fmt.Sprintf("/admin/documents/%d/delete", docToDelete), nil))

	// 5. Delete Game
	recDelGame := httptest.NewRecorder()
	h.HandleAdminGameRoute(recDelGame, httptest.NewRequest("DELETE", fmt.Sprintf("/admin/games/%d/delete", gameID), nil))

	gamesAfter, _ := db.ListGames("Terraforming Mars", 0)
	if len(gamesAfter) != 0 {
		t.Errorf("expected game to be deleted from database")
	}
}
