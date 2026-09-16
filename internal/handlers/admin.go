package handlers

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/christian-klein/bgtags/internal/backup"
	"github.com/christian-klein/bgtags/internal/bgg"
	"github.com/christian-klein/bgtags/internal/database"
	"github.com/christian-klein/bgtags/internal/pdf"
)

func (h *Handler) loadAdminGames(q string) ([]GameView, error) {
	games, err := h.db.ListGames(q, 0)
	if err != nil {
		return nil, err
	}

	views := make([]GameView, len(games))
	for i, g := range games {
		docs, err := h.db.ListDocuments(g.ID)
		if err != nil {
			docs = nil
		}
		views[i] = GameView{
			Game:      g,
			Documents: docs,
		}
	}
	return views, nil
}

func (h *Handler) HandleAdmin(w http.ResponseWriter, r *http.Request) {
	games, err := h.loadAdminGames("")
	if err != nil {
		log.Printf("Error loading games for admin: %v", err)
		http.Error(w, "Failed to load games", http.StatusInternalServerError)
		return
	}

	backups, err := backup.ListBackups(h.cfg.BackupDir)
	if err != nil {
		log.Printf("Error listing backups for admin: %v", err)
	}

	totalOpt, linOpt, pendingOpt, _ := h.db.GetOptimizationStats()
	allCollections, _ := h.db.ListAllCollections()

	baseURL := h.getBaseURL(r)
	data := PageData{
		Title:                "Admin Control Panel",
		Games:                games,
		TotalCount:           len(games),
		Backups:              backups,
		OptTotal:             totalOpt,
		OptLinearized:        linOpt,
		OptPending:           pendingOpt,
		Settings:             h.db.GetAdminSettings(),
		AvailableCollections: allCollections,
		BaseURL:              baseURL,
		ActiveNav:            "admin",
	}
	h.populateAuthData(r, &data)

	if err := h.pages["admin.html"].ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("Admin template render error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleAdminGames(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	games, err := h.loadAdminGames(q)
	if err != nil {
		log.Printf("Error filtering admin games: %v", err)
		http.Error(w, "Failed to filter games", http.StatusInternalServerError)
		return
	}

	data := PageData{
		Games:      games,
		TotalCount: len(games),
	}

	if err := h.partials.ExecuteTemplate(w, "admin_game_table.html", data); err != nil {
		log.Printf("Admin table partial error: %v", err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleAdminGameModal(w http.ResponseWriter, r *http.Request) {
	data := PageData{}
	if err := h.partials.ExecuteTemplate(w, "admin_game_modal.html", data); err != nil {
		log.Printf("Admin game modal render error: %v", err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleAdminDocModal(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/admin/games/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	gameID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.db.GetGame(gameID)
	if err != nil || game == nil {
		http.NotFound(w, r)
		return
	}

	data := PageData{
		Game: &GameView{Game: *game},
	}

	if err := h.partials.ExecuteTemplate(w, "admin_doc_modal.html", data); err != nil {
		log.Printf("Admin doc modal render error: %v", err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleAdminCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Form parsing error: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "Game name is required", http.StatusBadRequest)
		return
	}

	minPlayers, _ := strconv.Atoi(r.FormValue("min_players"))
	maxPlayers, _ := strconv.Atoi(r.FormValue("max_players"))
	bestPlayers := strings.TrimSpace(r.FormValue("best_players"))
	complexity, _ := strconv.ParseFloat(r.FormValue("complexity"), 64)
	rating, _ := strconv.ParseFloat(r.FormValue("rating"), 64)
	bggURL := strings.TrimSpace(r.FormValue("bgg_url"))

	var imageName string
	if file, header, err := r.FormFile("image_file"); err == nil && header.Size > 0 {
		defer file.Close()
		imgDir := filepath.Join(h.cfg.StaticDir, "img")
		savedName, err := saveUploadedFile(file, header.Filename, imgDir, []string{".jpg", ".jpeg", ".png", ".webp", ".svg"})
		if err != nil {
			http.Error(w, "Image upload failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		imageName = savedName
	} else {
		imageName = strings.TrimSpace(r.FormValue("image_name"))
	}

	var pdfName string
	if file, header, err := r.FormFile("pdf_file"); err == nil && header.Size > 0 {
		defer file.Close()
		rulesDir := filepath.Join(h.cfg.StaticDir, "rules")
		savedName, err := saveUploadedFile(file, header.Filename, rulesDir, []string{".pdf"})
		if err != nil {
			http.Error(w, "PDF upload failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		pdfName = savedName
	} else {
		pdfName = strings.TrimSpace(r.FormValue("pdf_name"))
	}

	game := &database.Game{
		Name:        name,
		Image:       imageName,
		URL:         pdfName,
		MinPlayers:  minPlayers,
		MaxPlayers:  maxPlayers,
		BestPlayers: bestPlayers,
		Complexity:  complexity,
		Rating:      rating,
		BggURL:      bggURL,
	}

	if err := h.db.CreateGame(game); err != nil {
		log.Printf("Failed to create game: %v", err)
		http.Error(w, "Failed to create game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if pdfName != "" {
		h.optimizeUploadedPDF(pdfName)
	}

	log.Printf("[bgtags] Created game #%d: %s", game.ID, game.Name)

	games, _ := h.loadAdminGames("")
	data := PageData{Games: games, TotalCount: len(games)}
	_ = h.partials.ExecuteTemplate(w, "admin_game_table.html", data)
}

func (h *Handler) HandleAdminGameRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/admin/games/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	gameID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if len(parts) >= 2 {
		action := parts[1]
		if action == "document-modal" {
			h.HandleAdminDocModal(w, r)
			return
		}
		if action == "delete" && (r.Method == http.MethodDelete || r.Method == http.MethodPost) {
			if err := h.db.DeleteGame(gameID); err != nil {
				log.Printf("Failed to delete game %d: %v", gameID, err)
				http.Error(w, "Delete failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			log.Printf("[bgtags] Deleted game #%d", gameID)
			games, _ := h.loadAdminGames("")
			data := PageData{Games: games, TotalCount: len(games)}
			_ = h.partials.ExecuteTemplate(w, "admin_game_table.html", data)
			return
		}
		if action == "documents" && r.Method == http.MethodPost {
			h.handleAdminAddDocument(w, r, gameID)
			return
		}
	}

	http.NotFound(w, r)
}

func (h *Handler) handleAdminAddDocument(w http.ResponseWriter, r *http.Request, gameID int64) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Form parse error: "+err.Error(), http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	category := strings.TrimSpace(r.FormValue("category"))
	isPrimary := r.FormValue("is_primary") == "true"

	if title == "" {
		http.Error(w, "Document title is required", http.StatusBadRequest)
		return
	}
	if category == "" {
		category = "reference"
	}

	var pdfName string
	if file, header, err := r.FormFile("pdf_file"); err == nil && header.Size > 0 {
		defer file.Close()
		rulesDir := filepath.Join(h.cfg.StaticDir, "rules")
		savedName, err := saveUploadedFile(file, header.Filename, rulesDir, []string{".pdf"})
		if err != nil {
			http.Error(w, "PDF upload failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		pdfName = savedName
	} else {
		pdfName = strings.TrimSpace(r.FormValue("pdf_name"))
	}

	if pdfName == "" {
		http.Error(w, "PDF file or filename is required", http.StatusBadRequest)
		return
	}

	doc := &database.GameDocument{
		GameID:    gameID,
		Title:     title,
		Category:  category,
		Filename:  pdfName,
		IsPrimary: isPrimary,
	}

	if err := h.db.AddDocument(doc); err != nil {
		log.Printf("Failed to add document to game %d: %v", gameID, err)
		http.Error(w, "Failed to add document: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.optimizeUploadedPDF(pdfName)

	log.Printf("[bgtags] Added document '%s' to game #%d", doc.Title, gameID)

	games, _ := h.loadAdminGames("")
	data := PageData{Games: games, TotalCount: len(games)}
	_ = h.partials.ExecuteTemplate(w, "admin_game_table.html", data)
}

func (h *Handler) HandleAdminDocumentRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/admin/documents/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	docID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	action := parts[1]
	if action == "delete" && (r.Method == http.MethodDelete || r.Method == http.MethodPost) {
		if err := h.db.DeleteDocument(docID); err != nil {
			log.Printf("Failed to delete document %d: %v", docID, err)
			http.Error(w, "Delete failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[bgtags] Deleted document #%d", docID)
		games, _ := h.loadAdminGames("")
		data := PageData{Games: games, TotalCount: len(games)}
		_ = h.partials.ExecuteTemplate(w, "admin_game_table.html", data)
		return
	}

	http.NotFound(w, r)
}

func saveUploadedFile(file multipart.File, originalName, targetDir string, allowedExts []string) (string, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	isAllowed := false
	for _, a := range allowedExts {
		if ext == a {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		return "", fmt.Errorf("file extension %s is not permitted", ext)
	}

	base := filepath.Base(originalName)
	stem := strings.TrimSuffix(base, ext)
	reg := regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)
	cleanStem := strings.ToLower(reg.ReplaceAllString(stem, "-"))
	cleanStem = strings.Trim(cleanStem, "-")
	if cleanStem == "" {
		cleanStem = "file"
	}

	filename := cleanStem + ext
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}

	targetPath := filepath.Join(targetDir, filename)
	// If file exists with same name, keep the target
	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", err
	}

	return filename, nil
}

func (h *Handler) optimizeUploadedPDF(filename string) {
	rulesDir := filepath.Join(h.cfg.StaticDir, "rules")
	fullPath := filepath.Join(rulesDir, filename)
	go func() {
		log.Printf("[pdf-optimizer] Running optimization for uploaded file %s...", filename)
		if err := pdf.LinearizeFile(fullPath); err != nil {
			log.Printf("[pdf-optimizer] Warning: linearize failed for %s: %v", filename, err)
			return
		}
		if stat, err := os.Stat(fullPath); err == nil {
			_ = h.db.SavePDFOptimization(&database.PDFOptimization{
				Filename:     filename,
				FileSize:     stat.Size(),
				ModTime:      stat.ModTime().Unix(),
				IsLinearized: true,
			})
			log.Printf("[pdf-optimizer] Uploaded file %s linearized successfully (%d bytes)", filename, stat.Size())
		}
	}()
}

func (h *Handler) HandleAdminOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rulesDir := filepath.Join(h.cfg.StaticDir, "rules")
	processed, skipped, err := pdf.SyncDirectory(h.db, rulesDir)
	if err != nil {
		log.Printf("[pdf-optimizer] Optimization error: %v", err)
		http.Error(w, "Optimization error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	total, lin, pending, _ := h.db.GetOptimizationStats()

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="optimization-result" style="animation: fadeIn 0.3s ease;">
		<div style="background: rgba(16, 185, 129, 0.15); border: 1px solid rgba(16, 185, 129, 0.3); color: #34d399; padding: 0.75rem 1rem; border-radius: 8px; margin-bottom: 0.75rem; font-size: 0.9rem;">
			✓ <strong>Optimization Complete:</strong> %d processed, %d skipped. All rulebooks are now optimized for Fast Web View!
		</div>
		<div style="display: flex; gap: 0.75rem; align-items: center;">
			<span class="badge" style="background: #10b981; color: white; padding: 0.35rem 0.75rem; border-radius: 6px; font-weight: 600;">%d Linearized</span>
			<span class="badge" style="background: rgba(255,255,255,0.1); color: var(--text-muted, #9ca3af); padding: 0.35rem 0.75rem; border-radius: 6px;">%d Pending</span>
			<span class="badge" style="background: rgba(255,255,255,0.05); color: var(--text-muted, #9ca3af); padding: 0.35rem 0.75rem; border-radius: 6px;">Total: %d</span>
		</div>
	</div>`, processed, skipped, lin, pending, total)
}

func (h *Handler) HandleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	hideVal := r.FormValue("hide_game_title_in_expansions")
	hideExp := hideVal == "true" || hideVal == "on" || hideVal == "1"
	if err := h.db.SetSettingBool("hide_game_title_in_expansions", hideExp); err != nil {
		log.Printf("Error saving admin settings: %v", err)
		h.renderSettingsFeedback(w, r, false, "Failed to save expansion display setting: "+err.Error())
		return
	}

	defaultColl := strings.TrimSpace(r.FormValue("default_collection"))
	if err := h.db.SetSetting("default_collection", defaultColl); err != nil {
		log.Printf("Error saving default_collection setting: %v", err)
		h.renderSettingsFeedback(w, r, false, "Failed to save default collection: "+err.Error())
		return
	}

	restrictVal := r.FormValue("restrict_shared_game_moves")
	restrictMoves := restrictVal == "true" || restrictVal == "on" || restrictVal == "1"
	if err := h.db.SetSettingBool("restrict_shared_game_moves", restrictMoves); err != nil {
		log.Printf("Error saving restrict_shared_game_moves setting: %v", err)
		h.renderSettingsFeedback(w, r, false, "Failed to save game move policy: "+err.Error())
		return
	}

	// Sanitize BGG token (strips v1:, Bearer, whitespace)
	bggToken := bgg.SanitizeToken(r.FormValue("bgg_api_token"))
	if err := h.db.SetSetting("bgg_api_token", bggToken); err != nil {
		log.Printf("Error saving bgg_api_token setting: %v", err)
		h.renderSettingsFeedback(w, r, false, "Failed to save BGG API token: "+err.Error())
		return
	}

	h.renderSettingsFeedback(w, r, true, "Settings saved successfully.")
}

func (h *Handler) renderSettingsFeedback(w http.ResponseWriter, r *http.Request, success bool, msg string) {
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if success {
			fmt.Fprintf(w, `<span id="settings-save-feedback" class="alert alert-success" style="display: inline-flex; align-items: center; gap: 0.4rem; padding: 0.4rem 0.85rem; margin: 0; font-size: 0.875rem; border-radius: 6px; animation: fadeIn 0.3s ease;">
				<span>✓</span> %s
			</span>
			<script>
				setTimeout(function() {
					var el = document.getElementById('settings-save-feedback');
					if (el) {
						el.style.transition = 'opacity 0.5s ease';
						el.style.opacity = '0';
						setTimeout(function() { if (el && el.parentNode) el.remove(); }, 500);
					}
				}, 3000);
			</script>`, templateEscape(msg))
		} else {
			fmt.Fprintf(w, `<span id="settings-save-feedback" class="alert alert-danger" style="display: inline-flex; align-items: center; gap: 0.4rem; padding: 0.4rem 0.85rem; margin: 0; font-size: 0.875rem; border-radius: 6px; animation: fadeIn 0.3s ease;">
				<span>✗</span> %s
			</span>
			<script>
				setTimeout(function() {
					var el = document.getElementById('settings-save-feedback');
					if (el) {
						el.style.transition = 'opacity 0.5s ease';
						el.style.opacity = '0';
						setTimeout(function() { if (el && el.parentNode) el.remove(); }, 500);
					}
				}, 6000);
			</script>`, templateEscape(msg))
		}
		return
	}

	http.Redirect(w, r, "/admin?tab=settings", http.StatusSeeOther)
}


