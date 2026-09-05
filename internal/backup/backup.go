package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/christian-klein/bgtags/internal/database"
)

const (
	CurrentBackupVersion = 1
	BackupFilePrefix     = "backup-"
	BackupFileExt        = ".json"
)

type BgtagsBackup struct {
	Version    int             `json:"version"`
	ExportedAt time.Time       `json:"exported_at"`
	Summary    BackupSummary   `json:"summary"`
	Games      []database.Game `json:"games"`
}

type BackupSummary struct {
	GameCount int `json:"game_count"`
}

type BackupFileMeta struct {
	FileName  string    `json:"file_name"`
	FilePath  string    `json:"file_path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	GameCount int       `json:"game_count"`
}

func CreateBackup(db *database.DB, destDir string, retention int) (*BackupFileMeta, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("creating backup directory: %w", err)
	}

	backup, err := ExportData(db)
	if err != nil {
		return nil, fmt.Errorf("exporting database data: %w", err)
	}

	timestamp := backup.ExportedAt.UTC().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("%s%s%s", BackupFilePrefix, timestamp, BackupFileExt)
	destPath := filepath.Join(destDir, filename)
	tmpPath := destPath + ".tmp"

	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling backup json: %w", err)
	}

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return nil, fmt.Errorf("writing temporary backup file: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("finalizing backup file rename: %w", err)
	}

	if retention > 0 {
		_, _ = PruneOldBackups(destDir, retention)
	}

	fileInfo, err := os.Stat(destPath)
	size := int64(len(data))
	if err == nil {
		size = fileInfo.Size()
	}

	return &BackupFileMeta{
		FileName:  filename,
		FilePath:  destPath,
		SizeBytes: size,
		CreatedAt: backup.ExportedAt,
		GameCount: backup.Summary.GameCount,
	}, nil
}

func ExportData(db *database.DB) (*BgtagsBackup, error) {
	now := time.Now().UTC()
	backup := &BgtagsBackup{
		Version:    CurrentBackupVersion,
		ExportedAt: now,
	}

	games, err := db.ListGames("", 0)
	if err != nil {
		return nil, fmt.Errorf("querying games: %w", err)
	}
	backup.Games = games
	backup.Summary = BackupSummary{
		GameCount: len(games),
	}

	return backup, nil
}

func PruneOldBackups(destDir string, retentionCount int) ([]string, error) {
	if retentionCount <= 0 {
		return nil, nil
	}

	files, err := os.ReadDir(destDir)
	if err != nil {
		return nil, fmt.Errorf("reading backup directory for prune: %w", err)
	}

	var backupFiles []string
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), BackupFilePrefix) && strings.HasSuffix(f.Name(), BackupFileExt) {
			backupFiles = append(backupFiles, f.Name())
		}
	}

	sort.Strings(backupFiles)

	var deleted []string
	if len(backupFiles) > retentionCount {
		excess := len(backupFiles) - retentionCount
		for i := 0; i < excess; i++ {
			toDelete := filepath.Join(destDir, backupFiles[i])
			if err := os.Remove(toDelete); err == nil {
				deleted = append(deleted, backupFiles[i])
			}
		}
	}

	return deleted, nil
}

func ListBackups(destDir string) ([]*BackupFileMeta, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	files, err := os.ReadDir(destDir)
	if err != nil {
		return nil, fmt.Errorf("listing backups: %w", err)
	}

	var results []*BackupFileMeta
	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), BackupFilePrefix) || !strings.HasSuffix(f.Name(), BackupFileExt) {
			continue
		}

		info, err := f.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(destDir, f.Name())
		meta := &BackupFileMeta{
			FileName:  f.Name(),
			FilePath:  fullPath,
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime(),
		}

		if data, err := os.ReadFile(fullPath); err == nil {
			var parsed BgtagsBackup
			if err := json.Unmarshal(data, &parsed); err == nil {
				meta.CreatedAt = parsed.ExportedAt
				meta.GameCount = parsed.Summary.GameCount
			}
		}

		results = append(results, meta)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	return results, nil
}

func RestoreBackup(db *database.DB, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading backup file '%s': %w", filePath, err)
	}
	return RestoreFromBytes(db, data)
}

func RestoreFromBytes(db *database.DB, data []byte) error {
	var backup BgtagsBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return fmt.Errorf("parsing backup json: %w", err)
	}

	if backup.Version != CurrentBackupVersion {
		return fmt.Errorf("unsupported backup version %d", backup.Version)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting restore transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM games;"); err != nil {
		return fmt.Errorf("clearing games table: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO games (id, name, url, image, min_players, max_players, bgg_url, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing insert statement: %w", err)
	}
	defer stmt.Close()

	for _, g := range backup.Games {
		if _, err := stmt.Exec(g.ID, g.Name, g.URL, g.Image, g.MinPlayers, g.MaxPlayers, g.BggURL, g.CreatedAt, g.UpdatedAt); err != nil {
			return fmt.Errorf("restoring game %s: %w", g.Name, err)
		}
	}

	return tx.Commit()
}

func FormatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
