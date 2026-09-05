package backup

import (
	"path/filepath"
	"testing"

	"github.com/christian-klein/bgtags/internal/database"
)

func TestBackupAndRestore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	backupDir := filepath.Join(tempDir, "backups")

	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Insert test games with BestPlayers and Complexity
	g1 := &database.Game{Name: "Game 1", URL: "g1.pdf", Image: "g1.webp", MinPlayers: 2, MaxPlayers: 4, BestPlayers: "4", Complexity: 3.5}
	g2 := &database.Game{Name: "Game 2", URL: "g2.pdf", Image: "g2.webp", MinPlayers: 1, MaxPlayers: 6, BestPlayers: "3", Complexity: 2.1}
	if err := db.CreateGame(g1); err != nil {
		t.Fatalf("failed to create g1: %v", err)
	}
	if err := db.CreateGame(g2); err != nil {
		t.Fatalf("failed to create g2: %v", err)
	}

	meta, err := CreateBackup(db, backupDir, 5)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	if meta.GameCount != 2 {
		t.Errorf("expected 2 games in backup, got %d", meta.GameCount)
	}

	backups, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(backups))
	}

	// Delete g2
	if err := db.DeleteGame(g2.ID); err != nil {
		t.Fatalf("failed to delete g2: %v", err)
	}
	remaining, err := db.ListGames("", 0)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("expected 1 remaining game, got %d", len(remaining))
	}

	// Restore from backup
	if err := RestoreBackup(db, meta.FilePath); err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	restored, err := db.ListGames("", 0)
	if err != nil {
		t.Fatalf("ListGames after restore failed: %v", err)
	}
	if len(restored) != 2 {
		t.Errorf("expected 2 games after restore, got %d", len(restored))
	}
	if restored[0].BestPlayers == "" || restored[0].Complexity == 0 {
		t.Errorf("expected complexity and best_players restored, got %+v", restored[0])
	}
}
