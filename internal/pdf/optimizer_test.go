package pdf

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/christian-klein/bgtags/internal/database"
)

const minimalPDF = `%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >> endobj
xref
0 4
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
trailer << /Size 4 /Root 1 0 R >>
startxref
190
%%EOF
`

func TestLinearizeAndCheck(t *testing.T) {
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("qpdf not installed, skipping test")
	}

	tempDir := t.TempDir()
	testPDF := filepath.Join(tempDir, "test.pdf")
	if err := os.WriteFile(testPDF, []byte(minimalPDF), 0644); err != nil {
		t.Fatalf("failed to write test PDF: %v", err)
	}

	// Should not be linearized initially
	isLin, err := CheckLinearized(testPDF)
	if err != nil {
		t.Fatalf("CheckLinearized failed: %v", err)
	}
	if isLin {
		t.Fatalf("expected test.pdf to not be linearized initially")
	}

	// Linearize it
	if err := LinearizeFile(testPDF); err != nil {
		t.Fatalf("LinearizeFile failed: %v", err)
	}

	// Now should be linearized
	isLinAfter, err := CheckLinearized(testPDF)
	if err != nil {
		t.Fatalf("CheckLinearized after failed: %v", err)
	}
	if !isLinAfter {
		t.Fatalf("expected test.pdf to be linearized after LinearizeFile")
	}
}

func TestSyncDirectory(t *testing.T) {
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("qpdf not installed, skipping test")
	}

	tempDir := t.TempDir()
	rulesDir := filepath.Join(tempDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatalf("failed to create rules dir: %v", err)
	}

	testPDF := filepath.Join(rulesDir, "sample.pdf")
	if err := os.WriteFile(testPDF, []byte(minimalPDF), 0644); err != nil {
		t.Fatalf("failed to write test PDF: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := database.Open(dbPath, "")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Initial sync: should process 1 file
	processed, skipped, err := SyncDirectory(db, rulesDir)
	if err != nil {
		t.Fatalf("SyncDirectory failed: %v", err)
	}
	if processed != 1 || skipped != 0 {
		t.Fatalf("expected processed=1, skipped=0, got processed=%d, skipped=%d", processed, skipped)
	}

	// Second sync: should skip 1 file
	processed2, skipped2, err := SyncDirectory(db, rulesDir)
	if err != nil {
		t.Fatalf("SyncDirectory second run failed: %v", err)
	}
	if processed2 != 0 || skipped2 != 1 {
		t.Fatalf("expected processed=0, skipped=1, got processed=%d, skipped=%d", processed2, skipped2)
	}

	// Stats check
	total, lin, pending, err := db.GetOptimizationStats()
	if err != nil {
		t.Fatalf("GetOptimizationStats failed: %v", err)
	}
	if total != 1 || lin != 1 || pending != 0 {
		t.Fatalf("expected total=1, lin=1, pending=0, got total=%d, lin=%d, pending=%d", total, lin, pending)
	}
}
