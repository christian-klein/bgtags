package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christian-klein/bgtags/internal/auth"
	"github.com/christian-klein/bgtags/internal/config"
	"github.com/christian-klein/bgtags/internal/database"
	"github.com/christian-klein/bgtags/internal/middleware"
)

func TestCollectionHandlers(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "collection_test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		Port:               "8081",
		StaticDir:          filepath.Join(tempDir, "static"),
		BackupDir:          filepath.Join(tempDir, "backups"),
		DefaultOwner:       "cdk2128",
		OIDCCollectionGroup: "bgtags-collectors",
		OIDCAdminGroup:     "bgtags-admins",
		LocalDevMode:       true,
	}

	h, err := New(db, cfg, "../../templates")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create sample games in library
	bggID1 := 1001
	g1 := &database.Game{
		Name:   "Catan",
		URL:    "catan.pdf",
		BggID:  &bggID1,
		Rating: 7.2,
	}
	if err := db.CreateGame(g1); err != nil {
		t.Fatalf("CreateGame g1 failed: %v", err)
	}

	bggID2 := 1002
	g2 := &database.Game{
		Name:   "Catan: Cities & Knights",
		URL:    "cities-knights.pdf",
		BggID:  &bggID2,
		Rating: 8.0,
	}
	if err := db.CreateGame(g2); err != nil {
		t.Fatalf("CreateGame g2 failed: %v", err)
	}

	// Helper to create request with authenticated collector
	collectorSession := &auth.SessionData{
		Username: "alice",
		Groups:   []string{"bgtags-collectors"},
	}

	// 1. Add game to alice's collection via /collection/add-existing
	form := url.Values{}
	form.Set("game_id", "1")
	reqAdd := httptest.NewRequest("POST", "/collection/add-existing", strings.NewReader(form.Encode()))
	reqAdd.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(reqAdd.Context(), middleware.UserContextKey, collectorSession)
	reqAdd = reqAdd.WithContext(ctx)
	wAdd := httptest.NewRecorder()

	h.HandleCollectionAddExisting(wAdd, reqAdd)
	if wAdd.Code != http.StatusOK {
		t.Fatalf("HandleCollectionAddExisting failed with status %d: %s", wAdd.Code, wAdd.Body.String())
	}
	if !strings.Contains(wAdd.Body.String(), "Added to your collection successfully") {
		t.Errorf("expected success message, got: %s", wAdd.Body.String())
	}

	// Verify game is in Alice's collection
	inAlice, err := db.IsGameInUserCollection("alice", g1.ID)
	if err != nil || !inAlice {
		t.Fatalf("expected game 1 in alice's collection")
	}

	// 2. Render /collection page
	reqCollection := httptest.NewRequest("GET", "/collection", nil)
	reqCollection = reqCollection.WithContext(context.WithValue(reqCollection.Context(), middleware.UserContextKey, collectorSession))
	wCollection := httptest.NewRecorder()

	h.HandleCollection(wCollection, reqCollection)
	if wCollection.Code != http.StatusOK {
		t.Fatalf("HandleCollection failed with status %d: %s", wCollection.Code, wCollection.Body.String())
	}
	body := wCollection.Body.String()
	if !strings.Contains(body, "My Collection") || !strings.Contains(body, "Catan") {
		t.Errorf("expected My Collection and Catan in rendered page, got: %s", body)
	}

	// 3. Open reparent modal
	reqModal := httptest.NewRequest("GET", "/collection/games/2/reparent-modal", nil)
	reqModal = reqModal.WithContext(context.WithValue(reqModal.Context(), middleware.UserContextKey, collectorSession))
	wModal := httptest.NewRecorder()

	h.HandleCollectionReparentModal(wModal, reqModal, g2.ID)
	if wModal.Code != http.StatusOK {
		t.Fatalf("HandleCollectionReparentModal failed with status %d: %s", wModal.Code, wModal.Body.String())
	}
	if !strings.Contains(wModal.Body.String(), "Reparent Game / Manage Expansion") {
		t.Errorf("expected reparent modal title, got: %s", wModal.Body.String())
	}

	// 4. Reparent g2 under g1
	reparentForm := url.Values{}
	reparentForm.Set("parent_id", "1")
	reqReparent := httptest.NewRequest("POST", "/collection/games/2/reparent", strings.NewReader(reparentForm.Encode()))
	reqReparent.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqReparent = reqReparent.WithContext(context.WithValue(reqReparent.Context(), middleware.UserContextKey, collectorSession))
	wReparent := httptest.NewRecorder()

	h.HandleCollectionReparent(wReparent, reqReparent, g2.ID)
	if wReparent.Code != http.StatusOK {
		t.Fatalf("HandleCollectionReparent failed with status %d: %s", wReparent.Code, wReparent.Body.String())
	}

	// Verify reparent in DB
	reloadedG2, _ := db.GetGame(g2.ID)
	if reloadedG2.ParentID == nil || *reloadedG2.ParentID != g1.ID {
		t.Fatalf("expected g2 parent to be %d, got %v", g1.ID, reloadedG2.ParentID)
	}

	// 5. Remove game from alice's collection
	reqRemove := httptest.NewRequest("POST", "/collection/games/1/remove", nil)
	reqRemove = reqRemove.WithContext(context.WithValue(reqRemove.Context(), middleware.UserContextKey, collectorSession))
	wRemove := httptest.NewRecorder()

	h.HandleCollectionRemove(wRemove, reqRemove, g1.ID)
	if wRemove.Code != http.StatusOK {
		t.Fatalf("HandleCollectionRemove failed with status %d", wRemove.Code)
	}

	// Verify removed from alice
	inAliceAfter, _ := db.IsGameInUserCollection("alice", g1.ID)
	if inAliceAfter {
		t.Errorf("expected game 1 removed from alice's collection")
	}

	// Verify game still exists in master library!
	stillInLibrary, err := db.GetGame(g1.ID)
	if err != nil || stillInLibrary == nil {
		t.Fatalf("game 1 should NOT be deleted from master games table")
	}

	// 6. Test Collection Rename Modal
	reqRenameModal := httptest.NewRequest("GET", "/collection/rename-modal", nil)
	reqRenameModal = reqRenameModal.WithContext(context.WithValue(reqRenameModal.Context(), middleware.UserContextKey, collectorSession))
	wRenameModal := httptest.NewRecorder()
	h.HandleCollectionRenameModal(wRenameModal, reqRenameModal)
	if wRenameModal.Code != http.StatusOK {
		t.Fatalf("HandleCollectionRenameModal failed: %d", wRenameModal.Code)
	}
	if !strings.Contains(wRenameModal.Body.String(), "Rename Collection") {
		t.Errorf("expected Rename Collection in modal, got: %s", wRenameModal.Body.String())
	}

	// 7. Test Collection Rename Action
	renameForm := url.Values{}
	renameForm.Set("custom_name", "Alice's Board Games")
	reqRename := httptest.NewRequest("POST", "/collection/rename", strings.NewReader(renameForm.Encode()))
	reqRename.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqRename = reqRename.WithContext(context.WithValue(reqRename.Context(), middleware.UserContextKey, collectorSession))
	wRename := httptest.NewRecorder()
	h.HandleCollectionRename(wRename, reqRename)
	if wRename.Code != http.StatusOK {
		t.Fatalf("HandleCollectionRename failed: %d", wRename.Code)
	}
	if wRename.Header().Get("HX-Redirect") != "/collection" {
		t.Errorf("expected HX-Redirect to /collection, got: %s", wRename.Header().Get("HX-Redirect"))
	}

	// Verify DB name updated
	savedName, _ := db.GetCollectionName("alice")
	if savedName != "Alice's Board Games" {
		t.Errorf("expected 'Alice's Board Games', got '%s'", savedName)
	}
}
