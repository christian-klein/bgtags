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

	// UI & HTMX endpoints
	mux.HandleFunc("/", h.HandleIndex)
	mux.HandleFunc("/games", h.HandleGames)
	mux.HandleFunc("/stickers", h.HandleStickers)
	mux.HandleFunc("/qr", h.HandleQR)
	mux.HandleFunc("/backups/modal", h.HandleBackupModal)
	mux.HandleFunc("/backups/create", h.HandleCreateBackup)
	mux.HandleFunc("/backups/restore", h.HandleRestoreBackup)
	mux.HandleFunc("/health", h.HandleHealth)

	// Static assets
	staticDir := cfg.StaticDir
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.Handle("/img/", http.StripPrefix("/img/", http.FileServer(http.Dir(filepath.Join(staticDir, "img")))))

	// Rules PDF handler with inline preview headers
	rulesDir := filepath.Join(staticDir, "rules")
	mux.HandleFunc("/rules/", func(w http.ResponseWriter, r *http.Request) {
		filename := filepath.Base(r.URL.Path)
		filePath := filepath.Join(rulesDir, filename)

		// Prevent path traversal
		if _, err := os.Stat(filePath); err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "inline; filename=\""+filename+"\"")
		http.ServeFile(w, r, filePath)
	})

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
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
