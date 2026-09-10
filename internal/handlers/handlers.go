package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"context"

	"github.com/christian-klein/bgtags/internal/auth"
	"github.com/christian-klein/bgtags/internal/backup"
	"github.com/christian-klein/bgtags/internal/config"
	"github.com/christian-klein/bgtags/internal/database"
	"github.com/christian-klein/bgtags/internal/middleware"
)

type Handler struct {
	db       *database.DB
	cfg      *config.Config
	pages    map[string]*template.Template
	partials *template.Template
	oidc     *auth.OIDCService
}

type PageData struct {
	Title       string
	Games       []GameView
	Game        *GameView
	ParentGame  *database.Game
	Documents   []database.GameDocument
	Expansions  []database.Game
	TotalCount      int
	TotalGames      int
	TotalExpansions int
	SearchQuery     string
	PlayerCount   int
	MinComplexity float64
	MaxComplexity float64
	BaseURL       string
	ActiveNav     string
	Backups       []*backup.BackupFileMeta
	Message     string
	Error       string

	// PDF Optimization
	OptTotal      int
	OptLinearized int
	OptPending    int

	// Settings
	Settings database.AdminSettings

	// Auth & RBAC
	OIDCEnabled     bool
	IsAuthenticated bool
	IsAdmin         bool
	User            *auth.SessionData
}

type GameView struct {
	database.Game
	RulesHubURL     string
	QRCodeURL       string
	PrimaryRulesURL string
	Documents       []database.GameDocument
	Expansions      []database.Game
	ParentGame      *database.Game
}

func New(db *database.DB, cfg *config.Config, tmplDir string) (*Handler, error) {
	funcMap := template.FuncMap{
		"formatSize": backup.FormatFileSize,
		"formatDate": func(t interface{}) string {
			if tm, ok := t.(database.Game); ok {
				return tm.CreatedAt.Format("Jan 02, 2006")
			}
			return ""
		},
	}

	layoutPath := filepath.Join(tmplDir, "layout.html")
	partialFiles, err := filepath.Glob(filepath.Join(tmplDir, "partials", "*.html"))
	if err != nil {
		return nil, fmt.Errorf("listing partial templates: %w", err)
	}

	pages := make(map[string]*template.Template)
	for _, pageName := range []string{"index.html", "stickers.html", "rules_hub.html", "admin.html"} {
		pagePath := filepath.Join(tmplDir, pageName)
		files := append([]string{layoutPath, pagePath}, partialFiles...)
		t, err := template.New("layout.html").Funcs(funcMap).ParseFiles(files...)
		if err != nil {
			return nil, fmt.Errorf("parsing page template %s: %w", pageName, err)
		}
		pages[pageName] = t
	}

	partialsTmpl, err := template.New("").Funcs(funcMap).ParseFiles(partialFiles...)
	if err != nil {
		return nil, fmt.Errorf("parsing partial templates: %w", err)
	}

	oidc, err := auth.NewOIDCService(context.Background(), cfg.OIDCIssuerURL, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCRedirectURL, cfg.OIDCEnabled)
	if err != nil {
		log.Printf("[bgtags] Warning: failed to initialize OIDC provider: %v", err)
	}

	return &Handler{
		db:       db,
		cfg:      cfg,
		pages:    pages,
		partials: partialsTmpl,
		oidc:     oidc,
	}, nil
}

func (h *Handler) getBaseURL(r *http.Request) string {
	if h.cfg.BaseURL != "" {
		return strings.TrimRight(h.cfg.BaseURL, "/")
	}
	proto := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		proto = "https"
	}
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	return fmt.Sprintf("%s://%s", proto, host)
}

func (h *Handler) toGameViews(games []database.Game, baseURL string) []GameView {
	views := make([]GameView, len(games))
	for i, g := range games {
		rulesHubURL := fmt.Sprintf("%s/games/%d/rules", baseURL, g.ID)
		qrURL := fmt.Sprintf("/qr?url=%s", rulesHubURL)
		primaryURL := fmt.Sprintf("%s/rules/%s", baseURL, g.URL)
		views[i] = GameView{
			Game:            g,
			RulesHubURL:     rulesHubURL,
			QRCodeURL:       qrURL,
			PrimaryRulesURL: primaryURL,
		}
	}
	return views
}

func (h *Handler) populateAuthData(r *http.Request, data *PageData) {
	data.OIDCEnabled = h.cfg.OIDCEnabled
	data.IsAuthenticated = middleware.IsAuthenticated(r, h.cfg)
	data.IsAdmin = middleware.IsAdmin(r, h.cfg)
	data.User = middleware.UserFromContext(r.Context())
}

func (h *Handler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query().Get("q")
	players, _ := strconv.Atoi(r.URL.Query().Get("players"))
	minComp := 0.0
	maxComp := 5.0
	if minStr := r.URL.Query().Get("min_complexity"); minStr != "" {
		if v, err := strconv.ParseFloat(minStr, 64); err == nil {
			minComp = v
		}
	}
	if maxStr := r.URL.Query().Get("max_complexity"); maxStr != "" {
		if v, err := strconv.ParseFloat(maxStr, 64); err == nil {
			maxComp = v
		}
	}

	games, err := h.db.ListBaseGames(q, players, minComp, maxComp)
	if err != nil {
		log.Printf("Error listing games: %v", err)
		http.Error(w, "Failed to load games", http.StatusInternalServerError)
		return
	}

	totalGames, totalExpansions, err := h.db.CountGamesAndExpansions()
	if err != nil {
		log.Printf("Error counting collection: %v", err)
	}

	baseURL := h.getBaseURL(r)
	data := PageData{
		Title:           "Board Game Rule Tags",
		Games:           h.toGameViews(games, baseURL),
		TotalCount:      len(games),
		TotalGames:      totalGames,
		TotalExpansions: totalExpansions,
		SearchQuery:     q,
		PlayerCount:     players,
		MinComplexity:   minComp,
		MaxComplexity:   maxComp,
		BaseURL:         baseURL,
		ActiveNav:       "catalog",
	}
	h.populateAuthData(r, &data)

	if err := h.pages["index.html"].ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleGameRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/games/")
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

	game, docs, expansions, err := h.db.GetGameWithDetails(gameID)
	if err != nil {
		log.Printf("Error loading game details for %d: %v", gameID, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if game == nil {
		http.NotFound(w, r)
		return
	}

	var parentGame *database.Game
	if game.ParentID != nil {
		p, err := h.db.GetGame(*game.ParentID)
		if err == nil {
			parentGame = p
		}
	}

	if h.db.GetSettingBool("hide_game_title_in_expansions", false) {
		for i := range expansions {
			expansions[i].DisplayName = database.FormatExpansionName(expansions[i].Name, game.Name)
		}
	}

	baseURL := h.getBaseURL(r)
	rulesHubURL := fmt.Sprintf("%s/games/%d/rules", baseURL, game.ID)
	primaryURL := fmt.Sprintf("%s/rules/%s", baseURL, game.URL)
	if len(docs) > 0 && docs[0].Filename != "" {
		primaryURL = fmt.Sprintf("%s/rules/%s", baseURL, docs[0].Filename)
	}

	gv := GameView{
		Game:            *game,
		RulesHubURL:     rulesHubURL,
		QRCodeURL:       fmt.Sprintf("/qr?url=%s", rulesHubURL),
		PrimaryRulesURL: primaryURL,
		Documents:       docs,
		Expansions:      expansions,
		ParentGame:      parentGame,
	}

	data := PageData{
		Title:      fmt.Sprintf("%s - Rules & Documents", game.Name),
		Game:       &gv,
		ParentGame: parentGame,
		Documents:  docs,
		Expansions: expansions,
		BaseURL:    baseURL,
		ActiveNav:  "rules",
	}
	h.populateAuthData(r, &data)

	if err := h.pages["rules_hub.html"].ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("Rules hub template execution error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleGames(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	players, _ := strconv.Atoi(r.URL.Query().Get("players"))
	minComp := 0.0
	maxComp := 5.0
	if minStr := r.URL.Query().Get("min_complexity"); minStr != "" {
		if v, err := strconv.ParseFloat(minStr, 64); err == nil {
			minComp = v
		}
	}
	if maxStr := r.URL.Query().Get("max_complexity"); maxStr != "" {
		if v, err := strconv.ParseFloat(maxStr, 64); err == nil {
			maxComp = v
		}
	}

	games, err := h.db.ListBaseGames(q, players, minComp, maxComp)
	if err != nil {
		log.Printf("Error listing games: %v", err)
		http.Error(w, "Failed to filter games", http.StatusInternalServerError)
		return
	}

	baseURL := h.getBaseURL(r)
	data := PageData{
		Games:         h.toGameViews(games, baseURL),
		TotalCount:    len(games),
		SearchQuery:   q,
		PlayerCount:   players,
		MinComplexity: minComp,
		MaxComplexity: maxComp,
		BaseURL:       baseURL,
	}

	if err := h.partials.ExecuteTemplate(w, "game_grid.html", data); err != nil {
		log.Printf("Partial template error: %v", err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleStickers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	players, _ := strconv.Atoi(r.URL.Query().Get("players"))

	games, err := h.db.ListGames(q, players)
	if err != nil {
		log.Printf("Error listing games for stickers: %v", err)
		http.Error(w, "Failed to load games", http.StatusInternalServerError)
		return
	}

	baseURL := h.getBaseURL(r)
	data := PageData{
		Title:       "Print Game Box Stickers",
		Games:       h.toGameViews(games, baseURL),
		TotalCount:  len(games),
		SearchQuery: q,
		PlayerCount: players,
		BaseURL:     baseURL,
		ActiveNav:   "stickers",
	}
	h.populateAuthData(r, &data)

	if err := h.pages["stickers.html"].ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleBackupModal(w http.ResponseWriter, r *http.Request) {
	backups, err := backup.ListBackups(h.cfg.BackupDir)
	if err != nil {
		log.Printf("Error listing backups: %v", err)
	}

	data := PageData{
		Backups: backups,
	}

	if err := h.partials.ExecuteTemplate(w, "backup_modal.html", data); err != nil {
		log.Printf("Backup modal template error: %v", err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	meta, err := backup.CreateBackup(h.db, h.cfg.BackupDir, h.cfg.BackupRetention)
	if err != nil {
		log.Printf("Manual backup error: %v", err)
		http.Error(w, "Backup creation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Manual backup created: %s (%d games)", meta.FileName, meta.GameCount)

	backups, _ := backup.ListBackups(h.cfg.BackupDir)
	data := PageData{
		Backups: backups,
		Message: fmt.Sprintf("Backup %s created successfully!", meta.FileName),
	}

	_ = h.partials.ExecuteTemplate(w, "backup_modal.html", data)
}

func (h *Handler) HandleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filename := r.FormValue("filename")
	if filename == "" || strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "Invalid backup filename", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(h.cfg.BackupDir, filename)
	if err := backup.RestoreBackup(h.db, filePath); err != nil {
		log.Printf("Restore error: %v", err)
		http.Error(w, "Restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Restored database from %s", filename)

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) HandleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	http.ServeFile(w, r, filepath.Join(h.cfg.StaticDir, "manifest.webmanifest"))
}

func (h *Handler) HandleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Service-Worker-Allowed", "/")
	http.ServeFile(w, r, filepath.Join(h.cfg.StaticDir, "js", "sw.js"))
}

