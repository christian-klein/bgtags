package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/christian-klein/bgtags/internal/backup"
	"github.com/christian-klein/bgtags/internal/config"
	"github.com/christian-klein/bgtags/internal/database"
	"github.com/christian-klein/bgtags/internal/handlers"
	"github.com/christian-klein/bgtags/internal/middleware"
	"github.com/christian-klein/bgtags/internal/pdf"
)

func main() {
	cfg := config.Load()

	log.Printf("[bgtags] Starting server on port %s...", cfg.Port)
	log.Printf("[bgtags] DB Path: %s, Data Dir: %s, Static Dir: %s", cfg.DBPath, cfg.DataDir, cfg.StaticDir)

	seedPath := filepath.Join(cfg.DataDir, "seed_games.json")
	db, err := database.Open(cfg.DBPath, seedPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize and start background backup scheduler
	if cfg.BackupEnabled {
		scheduler := backup.NewScheduler(db, cfg.BackupDir, cfg.BackupTime, cfg.BackupRetention)
		scheduler.Start(ctx)
		defer scheduler.Stop()
	}

	// Initialize template handlers
	h, err := handlers.New(db, cfg, "templates")
	if err != nil {
		log.Fatalf("Failed to initialize handlers: %v", err)
	}

	mux := http.NewServeMux()

	// Authentication endpoints
	mux.HandleFunc("/login", h.HandleLogin)
	mux.HandleFunc("/auth/callback", h.HandleAuthCallback)
	mux.HandleFunc("/logout", h.HandleLogout)

	// UI & HTMX endpoints
	mux.HandleFunc("/", h.HandleIndex)
	mux.HandleFunc("/games", h.HandleGames)
	mux.HandleFunc("/games/", middleware.RequireReader(cfg, h.HandleGameRoute))
	mux.HandleFunc("/stickers", middleware.RequireReader(cfg, h.HandleStickers))
	mux.HandleFunc("/qr", h.HandleQR)
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/manifest.webmanifest", h.HandleManifest)
	mux.HandleFunc("/sw.js", h.HandleServiceWorker)

	// Admin endpoints (RBAC protected)
	mux.HandleFunc("/admin", middleware.RequireAdmin(cfg, h.HandleAdmin))
	mux.HandleFunc("/admin/games", middleware.RequireAdmin(cfg, h.HandleAdminGames))
	mux.HandleFunc("/admin/games/modal", middleware.RequireAdmin(cfg, h.HandleAdminGameModal))
	mux.HandleFunc("/admin/games/create", middleware.RequireAdmin(cfg, h.HandleAdminCreateGame))
	mux.HandleFunc("/admin/games/", middleware.RequireAdmin(cfg, h.HandleAdminGameRoute))
	mux.HandleFunc("/admin/documents/", middleware.RequireAdmin(cfg, h.HandleAdminDocumentRoute))
	mux.HandleFunc("/admin/optimize", middleware.RequireAdmin(cfg, h.HandleAdminOptimize))
	mux.HandleFunc("/admin/settings", middleware.RequireAdmin(cfg, h.HandleAdminSettings))
	mux.HandleFunc("/backups/create", middleware.RequireAdmin(cfg, h.HandleCreateBackup))
	mux.HandleFunc("/backups/restore", middleware.RequireAdmin(cfg, h.HandleRestoreBackup))

	// Static assets
	staticDir := cfg.StaticDir
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.Handle("/img/", http.StripPrefix("/img/", http.FileServer(http.Dir(filepath.Join(staticDir, "img")))))

	// Rules PDF handler with inline preview headers & streaming timeout exemption (reader protected)
	rulesDir := filepath.Join(staticDir, "rules")
	rulesHandler := func(w http.ResponseWriter, r *http.Request) {
		filename := filepath.Base(r.URL.Path)
		filePath := filepath.Join(rulesDir, filename)

		// Prevent path traversal
		if _, err := os.Stat(filePath); err != nil {
			http.NotFound(w, r)
			return
		}

		// Disable write timeout for large streaming file downloads
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "inline; filename=\""+filename+"\"")
		w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
		http.ServeFile(w, r, filePath)
	}
	mux.HandleFunc("/rules/", middleware.RequireReader(cfg, rulesHandler))

	// Background initial scan to ensure all rulebooks are linearized for Fast Web View
	go func() {
		log.Printf("[bgtags] Starting background rulebook optimization scan...")
		processed, skipped, err := pdf.SyncDirectory(db, rulesDir)
		if err != nil {
			log.Printf("[bgtags] Background rulebook optimization error: %v", err)
		} else {
			log.Printf("[bgtags] Background rulebook optimization complete: %d processed, %d already optimal", processed, skipped)
		}
	}()

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      middleware.WithUserContext(cfg)(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[bgtags] Listening on http://0.0.0.0:%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stop
	log.Println("[bgtags] Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("[bgtags] Server stopped.")
}
