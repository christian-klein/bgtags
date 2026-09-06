package pdf

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/christian-klein/bgtags/internal/database"
)

// CheckLinearized checks if a PDF file is linearized (Fast Web View).
func CheckLinearized(filePath string) (bool, error) {
	cmd := exec.Command("qpdf", "--check-linearization", filePath)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		// Exit code 0 = linearized, 3 = valid PDF but not linearized
		if exitErr.ExitCode() == 3 || exitErr.ExitCode() == 2 {
			return false, nil
		}
		return false, fmt.Errorf("qpdf check failed with exit code %d", exitErr.ExitCode())
	}
	return false, fmt.Errorf("running qpdf: %w", err)
}

// LinearizeFile reorganizes a PDF file in-place using qpdf --warning-exit-0 --linearize --replace-input.
func LinearizeFile(filePath string) error {
	cmd := exec.Command("qpdf", "--warning-exit-0", "--linearize", "--replace-input", filePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qpdf linearize failed: %s: %w", string(output), err)
	}

	// Ensure file remains readable and writable across container and host processes
	_ = os.Chmod(filePath, 0666)
	return nil
}

// SyncDirectory scans a directory of PDFs and linearizes any files that are new,
// modified, or not yet linearized, updating the SQLite tracking database.
func SyncDirectory(db *database.DB, rulesDir string) (processed int, skipped int, err error) {
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return 0, 0, fmt.Errorf("reading rules directory %s: %w", rulesDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
			continue
		}

		fullPath := filepath.Join(rulesDir, name)
		stat, err := os.Stat(fullPath)
		if err != nil {
			log.Printf("[pdf-optimizer] Warning: stat failed for %s: %v", name, err)
			continue
		}

		size := stat.Size()
		modTime := stat.ModTime().Unix()

		// Check tracking record in SQLite
		opt, err := db.GetPDFOptimization(name)
		if err != nil {
			log.Printf("[pdf-optimizer] Warning: db lookup failed for %s: %v", name, err)
		}

		if opt != nil && opt.FileSize == size && opt.ModTime == modTime && opt.IsLinearized {
			skipped++
			continue
		}

		// Check if already linearized
		isLin, err := CheckLinearized(fullPath)
		if err != nil {
			// If qpdf binary is missing or failed to run, log and stop loop
			log.Printf("[pdf-optimizer] Warning checking %s: %v", name, err)
			continue
		}

		if isLin {
			_ = db.SavePDFOptimization(&database.PDFOptimization{
				Filename:     name,
				FileSize:     size,
				ModTime:      modTime,
				IsLinearized: true,
			})
			processed++
			continue
		}

		// File is not linearized: linearize it
		log.Printf("[pdf-optimizer] Linearizing %s (size: %d bytes)...", name, size)
		if err := LinearizeFile(fullPath); err != nil {
			log.Printf("[pdf-optimizer] Error linearizing %s: %v", name, err)
			_ = db.SavePDFOptimization(&database.PDFOptimization{
				Filename:     name,
				FileSize:     size,
				ModTime:      modTime,
				IsLinearized: false,
			})
			continue
		}

		newStat, err := os.Stat(fullPath)
		newSize := size
		newModTime := modTime
		if err == nil {
			newSize = newStat.Size()
			newModTime = newStat.ModTime().Unix()
		}

		_ = db.SavePDFOptimization(&database.PDFOptimization{
			Filename:     name,
			FileSize:     newSize,
			ModTime:      newModTime,
			IsLinearized: true,
		})
		log.Printf("[pdf-optimizer] Successfully linearized %s (new size: %d bytes)", name, newSize)
		processed++
	}

	return processed, skipped, nil
}
