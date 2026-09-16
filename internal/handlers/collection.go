package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/christian-klein/bgtags/internal/bgg"
	"github.com/christian-klein/bgtags/internal/database"
	"github.com/christian-klein/bgtags/internal/middleware"
)

var filenameRegexp = regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)

func sanitizeFilename(s string) string {
	clean := filenameRegexp.ReplaceAllString(strings.ToLower(s), "-")
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return "game"
	}
	return clean
}

type BGGSearchResultView struct {
	ID            int
	Name          string
	YearPublished int
	InLibrary     bool
	InCollection  bool
	ExistingGame  *database.Game
}

type ReparentModalData struct {
	Game         *database.Game
	CurrentParent *database.Game
	Candidates   []database.Game
	SharedCount  int
	SharedUsers  []string
	IsRestricted bool
	IsAdmin      bool
}

// HandleCollection renders the My Collection page for collectors
func (h *Handler) HandleCollection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r, h.cfg)
	if userID == "" {
		http.Error(w, "Unauthorized: Collector identity required", http.StatusUnauthorized)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	sortBy := r.URL.Query().Get("sort_by")
	if sortBy == "" {
		sortBy = "name"
	}
	sortOrder := r.URL.Query().Get("sort_order")
	if sortOrder == "" {
		sortOrder = "asc"
	}

	games, err := h.db.ListBaseGames(q, 0, 0, 0, sortBy, sortOrder, userID)
	if err != nil {
		log.Printf("Error loading collection games for user %s: %v", userID, err)
		http.Error(w, "Failed to load collection", http.StatusInternalServerError)
		return
	}

	views := make([]GameView, len(games))
	baseURL := h.getBaseURL(r)
	for i, g := range games {
		docs, _ := h.db.ListDocuments(g.ID)
		exps, _ := h.db.ListExpansions(g.ID)
		sharedCount, sharedUsers, _ := h.db.GetGameSharedUserCount(g.ID, userID)

		rulesHubURL := fmt.Sprintf("%s/games/%d/rules", baseURL, g.ID)
		views[i] = GameView{
			Game:            g,
			RulesHubURL:     rulesHubURL,
			QRCodeURL:       fmt.Sprintf("/qr?url=%s", rulesHubURL),
			PrimaryRulesURL: fmt.Sprintf("%s/rules/%s", baseURL, g.URL),
			Documents:       docs,
			Expansions:      exps,
			SharedCount:     sharedCount,
			SharedUsers:     sharedUsers,
		}
	}

	totalGames, totalExpansions, err := h.db.CountGamesAndExpansions(userID)
	if err != nil {
		log.Printf("Error counting collection for %s: %v", userID, err)
	}

	collName, _ := h.db.GetCollectionName(userID)
	data := PageData{
		Title:                 "My Collection",
		Games:                 views,
		TotalCount:            len(views),
		TotalGames:            totalGames,
		TotalExpansions:       totalExpansions,
		SearchQuery:           q,
		SortBy:                sortBy,
		SortOrder:             sortOrder,
		CurrentCollectionUser: userID,
		CollectionDisplayName: collName,
		BaseURL:               baseURL,
		ActiveNav:             "collection",
		Settings:              h.db.GetAdminSettings(),
	}
	h.populateAuthData(r, &data)

	// If HTMX partial request for the table
	if r.Header.Get("HX-Request") != "" && r.URL.Query().Get("table_only") == "true" {
		if err := h.partials.ExecuteTemplate(w, "collection_game_table.html", data); err != nil {
			log.Printf("Collection table render error: %v", err)
			http.Error(w, "Render error", http.StatusInternalServerError)
		}
		return
	}

	if err := h.pages["collection.html"].ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("Collection page render error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleCollectionModal opens the Add Game modal
func (h *Handler) HandleCollectionModal(w http.ResponseWriter, r *http.Request) {
	data := PageData{}
	h.populateAuthData(r, &data)
	if err := h.partials.ExecuteTemplate(w, "bgg_search_modal.html", data); err != nil {
		log.Printf("BGG modal render error: %v", err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}

func (h *Handler) getBGGToken() string {
	tok := ""
	if dbTok, err := h.db.GetSetting("bgg_api_token", ""); err == nil && dbTok != "" {
		tok = dbTok
	} else {
		tok = h.cfg.BGGApiToken
	}
	return bgg.SanitizeToken(tok)
}

// HandleCollectionSearchBGG searches BGG and correlates results with the library and user collection
func (h *Handler) HandleCollectionSearchBGG(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r, h.cfg)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="bgg-search-empty">Type a board game name above to search BoardGameGeek.</div>`))
		return
	}

	client := bgg.NewClient(h.getBGGToken())
	results, err := client.Search(q)
	if err != nil {
		log.Printf("BGG Search error for query '%s': %v", q, err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<div class="alert alert-danger" style="margin: 0.5rem 0;">BGG Search failed: %s. You can still add manually if needed.</div>`, err.Error())
		return
	}

	if len(results) == 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="bgg-search-empty">No results found on BoardGameGeek for "<strong>` + templateEscape(q) + `</strong>".</div>`))
		return
	}

	searchViews := make([]BGGSearchResultView, len(results))
	for i, item := range results {
		var inLib bool
		var inColl bool
		var existingGame *database.Game

		// Check deduplication by BggID
		existing, err := h.db.FindGameByBggID(item.ID)
		if err == nil && existing != nil {
			inLib = true
			existingGame = existing
			inColl, _ = h.db.IsGameInUserCollection(userID, existing.ID)
		}

		searchViews[i] = BGGSearchResultView{
			ID:            item.ID,
			Name:          item.Name,
			YearPublished: item.YearPublished,
			InLibrary:     inLib,
			InCollection:  inColl,
			ExistingGame:  existingGame,
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderBGGSearchResults(w, searchViews)
}

// HandleCollectionSelectBGG handles picking a specific BGG result (fetching details or showing 1-click add)
func (h *Handler) HandleCollectionSelectBGG(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r, h.cfg)
	bggIDStr := r.URL.Query().Get("bgg_id")
	bggID, err := strconv.Atoi(bggIDStr)
	if err != nil || bggID <= 0 {
		http.Error(w, "Invalid BGG ID", http.StatusBadRequest)
		return
	}

	// 1. Check if it's already in the master library
	existing, err := h.db.FindGameByBggID(bggID)
	if err == nil && existing != nil {
		inColl, _ := h.db.IsGameInUserCollection(userID, existing.ID)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		renderExistingGameCard(w, existing, inColl)
		return
	}

	// 2. Not in library: fetch full details from BGG XMLAPI2
	client := bgg.NewClient(h.getBGGToken())
	details, err := client.GetThingDetails(bggID)
	if err != nil {
		log.Printf("Failed to fetch BGG details for ID %d: %v", bggID, err)
		http.Error(w, "Failed to load details from BGG: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch base games as candidate parents for expansion designation
	baseGames, _ := h.db.ListBaseGames("", 0, 0, 0, "name", "asc", "")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderBGGImportForm(w, details, baseGames)
}

// HandleCollectionAddExisting handles 1-click addition of an existing library game to user's collection
func (h *Handler) HandleCollectionAddExisting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := middleware.CurrentUserID(r, h.cfg)
	gameIDStr := r.FormValue("game_id")
	gameID, err := strconv.ParseInt(gameIDStr, 10, 64)
	if err != nil || gameID <= 0 {
		http.Error(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	if err := h.db.AddGameToUserCollection(userID, gameID); err != nil {
		log.Printf("Failed to add game %d to collection of %s: %v", gameID, userID, err)
		http.Error(w, "Failed to add to collection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "collectionUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`
		<div class="alert alert-success" style="margin: 0.5rem 0;">
			✓ Added to your collection successfully!
		</div>
		<script>
			setTimeout(function() { closeModal(); }, 800);
		</script>
	`))
}

// HandleCollectionImport handles uploading a rulebook PDF, downloading BGG box art, and creating a new game
func (h *Handler) HandleCollectionImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := middleware.CurrentUserID(r, h.cfg)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Form parsing error: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "Game name is required", http.StatusBadRequest)
		return
	}

	bggID, _ := strconv.Atoi(r.FormValue("bgg_id"))
	minPlayers, _ := strconv.Atoi(r.FormValue("min_players"))
	maxPlayers, _ := strconv.Atoi(r.FormValue("max_players"))
	bestPlayers := strings.TrimSpace(r.FormValue("best_players"))
	complexity, _ := strconv.ParseFloat(r.FormValue("complexity"), 64)
	rating, _ := strconv.ParseFloat(r.FormValue("rating"), 64)
	bggURL := strings.TrimSpace(r.FormValue("bgg_url"))
	bggImageURL := strings.TrimSpace(r.FormValue("bgg_image_url"))

	var parentID *int64
	if parentIDStr := strings.TrimSpace(r.FormValue("parent_id")); parentIDStr != "" && parentIDStr != "0" {
		if pid, err := strconv.ParseInt(parentIDStr, 10, 64); err == nil && pid > 0 {
			parentID = &pid
		}
	}

	// 1. Check deduplication again before insert
	if bggID > 0 {
		existing, _ := h.db.FindGameByBggID(bggID)
		if existing != nil {
			_ = h.db.AddGameToUserCollection(userID, existing.ID)
			w.Header().Set("HX-Trigger", "collectionUpdated")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<div class="alert alert-success">Game was already in library! Linked to your collection.</div><script>setTimeout(closeModal, 800);</script>`))
			return
		}
	}

	// 2. Handle box art image
	var imageName string
	imgDir := filepath.Join(h.cfg.StaticDir, "img")
	if file, header, err := r.FormFile("image_file"); err == nil && header.Size > 0 {
		defer file.Close()
		savedName, err := saveUploadedFile(file, header.Filename, imgDir, []string{".jpg", ".jpeg", ".png", ".webp", ".svg"})
		if err == nil {
			imageName = savedName
		}
	} else if bggImageURL != "" {
		downloaded, err := downloadAndSaveImage(bggImageURL, imgDir, name)
		if err == nil {
			imageName = downloaded
		} else {
			log.Printf("Could not download cover image from BGG: %v", err)
		}
	}

	// 3. Handle rulebook PDF
	var pdfName string
	rulesDir := filepath.Join(h.cfg.StaticDir, "rules")
	if file, header, err := r.FormFile("pdf_file"); err == nil && header.Size > 0 {
		defer file.Close()
		savedName, err := saveUploadedFile(file, header.Filename, rulesDir, []string{".pdf"})
		if err != nil {
			http.Error(w, "PDF upload failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		pdfName = savedName
	} else {
		// If no PDF uploaded, create a standard placeholder name
		pdfName = sanitizeFilename(name) + ".pdf"
	}

	var bggIDPtr *int
	if bggID > 0 {
		bggIDPtr = &bggID
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
		BggID:       bggIDPtr,
		ParentID:    parentID,
	}

	if err := h.db.CreateGame(game); err != nil {
		log.Printf("Failed to create game %s: %v", name, err)
		http.Error(w, "Failed to create game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Link rule document
	_ = h.db.AddDocument(&database.GameDocument{
		GameID:    game.ID,
		Title:     "Core Rulebook",
		Category:  "core",
		Filename:  pdfName,
		IsPrimary: true,
	})

	// Optimize PDF in background
	if pdfName != "" {
		h.optimizeUploadedPDF(pdfName)
	}

	// Automatically add to user's collection
	_ = h.db.AddGameToUserCollection(userID, game.ID)

	log.Printf("[bgtags] User %s imported game #%d: %s", userID, game.ID, game.Name)

	w.Header().Set("HX-Trigger", "collectionUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`
		<div class="alert alert-success" style="margin: 0.5rem 0;">
			✓ Successfully added "<strong>` + templateEscape(game.Name) + `</strong>" to your collection!
		</div>
		<script>
			setTimeout(function() { closeModal(); }, 800);
		</script>
	`))
}

// HandleCollectionGameRoute routes sub-actions on a specific game in My Collection
func (h *Handler) HandleCollectionGameRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/collection/games/")
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
		switch action {
		case "reparent-modal":
			h.HandleCollectionReparentModal(w, r, gameID)
			return
		case "reparent":
			if r.Method == http.MethodPost {
				h.HandleCollectionReparent(w, r, gameID)
				return
			}
		case "remove":
			if r.Method == http.MethodPost || r.Method == http.MethodDelete {
				h.HandleCollectionRemove(w, r, gameID)
				return
			}
		}
	}

	http.NotFound(w, r)
}

// HandleCollectionReparentModal displays the modal to change a game's parent or make it standalone
func (h *Handler) HandleCollectionReparentModal(w http.ResponseWriter, r *http.Request, gameID int64) {
	userID := middleware.CurrentUserID(r, h.cfg)
	isAdmin := middleware.IsAdmin(r, h.cfg)

	game, err := h.db.GetGame(gameID)
	if err != nil || game == nil {
		http.NotFound(w, r)
		return
	}

	var currentParent *database.Game
	if game.ParentID != nil {
		currentParent, _ = h.db.GetGame(*game.ParentID)
	}

	sharedCount, sharedUsers, _ := h.db.GetGameSharedUserCount(gameID, userID)
	settings := h.db.GetAdminSettings()
	isRestricted := settings.RestrictSharedGameMoves && sharedCount > 0 && !isAdmin

	candidates, _ := h.db.ListBaseGames("", 0, 0, 0, "name", "asc", "")
	// Filter out the game itself
	filteredCandidates := make([]database.Game, 0, len(candidates))
	for _, c := range candidates {
		if c.ID != game.ID {
			filteredCandidates = append(filteredCandidates, c)
		}
	}

	modalData := ReparentModalData{
		Game:          game,
		CurrentParent: currentParent,
		Candidates:    filteredCandidates,
		SharedCount:   sharedCount,
		SharedUsers:   sharedUsers,
		IsRestricted:  isRestricted,
		IsAdmin:       isAdmin,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderReparentModal(w, modalData)
}

// HandleCollectionReparent updates a game's parent (or unparents to root)
func (h *Handler) HandleCollectionReparent(w http.ResponseWriter, r *http.Request, gameID int64) {
	userID := middleware.CurrentUserID(r, h.cfg)
	isAdmin := middleware.IsAdmin(r, h.cfg)

	sharedCount, _, _ := h.db.GetGameSharedUserCount(gameID, userID)
	settings := h.db.GetAdminSettings()

	if settings.RestrictSharedGameMoves && sharedCount > 0 && !isAdmin {
		http.Error(w, "Policy restriction: Only administrators can move games shared with other users.", http.StatusForbidden)
		return
	}

	parentVal := strings.TrimSpace(r.FormValue("parent_id"))
	var newParentID *int64
	if parentVal != "" && parentVal != "0" {
		pid, err := strconv.ParseInt(parentVal, 10, 64)
		if err == nil && pid > 0 && pid != gameID {
			newParentID = &pid
		}
	}

	if err := h.db.UpdateGameParent(gameID, newParentID); err != nil {
		log.Printf("Error updating parent for game %d: %v", gameID, err)
		http.Error(w, "Failed to reparent game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "collectionUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`
		<div class="alert alert-success" style="margin: 0.5rem 0;">
			✓ Game position updated successfully!
		</div>
		<script>
			setTimeout(function() { closeModal(); }, 800);
		</script>
	`))
}

// HandleCollectionRemove removes a game from the user's personal collection (without deleting from library)
func (h *Handler) HandleCollectionRemove(w http.ResponseWriter, r *http.Request, gameID int64) {
	userID := middleware.CurrentUserID(r, h.cfg)
	if err := h.db.RemoveGameFromUserCollection(userID, gameID); err != nil {
		log.Printf("Error removing game %d from collection of %s: %v", gameID, userID, err)
		http.Error(w, "Failed to remove game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "collectionUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(""))
}

// Helper to download cover image from BGG URL
func downloadAndSaveImage(imageURL, targetDir, fallbackName string) (string, error) {
	if imageURL == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "bgtags/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ext := ".jpg"
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "png") {
		ext = ".png"
	} else if strings.Contains(ct, "webp") {
		ext = ".webp"
	}

	cleanBase := sanitizeFilename(fallbackName)
	if cleanBase == "" {
		cleanBase = "bgg-cover"
	}
	finalName := fmt.Sprintf("%s%s", cleanBase, ext)
	dstPath := filepath.Join(targetDir, finalName)

	idx := 1
	for {
		if _, err := os.Stat(dstPath); os.IsNotExist(err) {
			break
		}
		finalName = fmt.Sprintf("%s-%d%s", cleanBase, idx, ext)
		dstPath = filepath.Join(targetDir, finalName)
		idx++
	}

	out, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", err
	}

	return finalName, nil
}

func templateEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func renderBGGSearchResults(w io.Writer, results []BGGSearchResultView) {
	fmt.Fprintf(w, `<div class="bgg-results-list" style="max-height: 380px; overflow-y: auto; display: flex; flex-direction: column; gap: 0.5rem;">`)
	for _, r := range results {
		fmt.Fprintf(w, `<div class="bgg-result-card" style="display: flex; align-items: center; justify-content: space-between; padding: 0.75rem 1rem; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: var(--radius-md); gap: 1rem;">
			<div style="flex: 1; min-width: 0;">
				<div style="font-weight: 600; color: var(--text-primary); font-size: 1rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
					%s
				</div>
				<div style="font-size: 0.85rem; color: var(--text-secondary); margin-top: 2px;">
					%s • BGG ID: #%d
				</div>
			</div>
			<div>`,
			templateEscape(r.Name),
			func() string {
				if r.YearPublished > 0 {
					return fmt.Sprintf("Released %d", r.YearPublished)
				}
				return "Year Unknown"
			}(),
			r.ID,
		)

		if r.InCollection {
			fmt.Fprintf(w, `<span class="badge" style="background: rgba(34, 197, 94, 0.2); color: #22c55e; border: 1px solid #22c55e; padding: 4px 10px; border-radius: 999px; font-size: 0.85rem; font-weight: 600;">✓ In My Collection</span>`)
		} else if r.InLibrary {
			fmt.Fprintf(w, `
				<button class="btn btn-sm btn-primary" 
						hx-post="/collection/add-existing" 
						hx-vals='{"game_id": "%d"}' 
						hx-target="#bgg-search-feedback"
						style="white-space: nowrap;">
					➕ Add to Collection
				</button>`, r.ExistingGame.ID)
		} else {
			fmt.Fprintf(w, `
				<button class="btn btn-sm btn-secondary" 
						hx-get="/collection/select-bgg?bgg_id=%d" 
						hx-target="#bgg-modal-body"
						style="white-space: nowrap;">
					⬇️ Import Game
				</button>`, r.ID)
		}

		fmt.Fprintf(w, `</div></div>`)
	}
	fmt.Fprintf(w, `</div><div id="bgg-search-feedback" style="margin-top: 0.75rem;"></div>`)
}

func renderExistingGameCard(w io.Writer, game *database.Game, inCollection bool) {
	fmt.Fprintf(w, `
	<div class="existing-game-preview" style="padding: 1rem; background: var(--bg-card); border-radius: var(--radius-md); border: 1px solid var(--border-color);">
		<h4 style="margin-top: 0; margin-bottom: 0.5rem; color: var(--accent-primary);">Found in Library!</h4>
		<p style="margin-bottom: 1rem; color: var(--text-secondary); font-size: 0.95rem;">
			"<strong>%s</strong>" already exists in the master library. You don't need to re-upload rules or cover art.
		</p>
		<div style="display: flex; gap: 1rem; align-items: center; margin-bottom: 1.25rem;">
			%s
			<div>
				<div style="font-weight: 600; font-size: 1.05rem;">%s</div>
				<div style="font-size: 0.85rem; color: var(--text-muted);">Players: %d–%d • Weight: %.2f</div>
			</div>
		</div>`,
		templateEscape(game.Name),
		func() string {
			if game.Image != "" {
				return fmt.Sprintf(`<img src="/img/%s" alt="%s" style="width: 50px; height: 50px; object-fit: cover; border-radius: 6px;">`, game.Image, templateEscape(game.Name))
			}
			return ""
		}(),
		templateEscape(game.Name),
		game.MinPlayers, game.MaxPlayers, game.Complexity,
	)

	if inCollection {
		fmt.Fprintf(w, `<div class="alert alert-info">This game is already in your personal collection!</div>`)
	} else {
		fmt.Fprintf(w, `
		<div style="display: flex; gap: 0.75rem;">
			<button class="btn btn-primary" 
					hx-post="/collection/add-existing" 
					hx-vals='{"game_id": "%d"}' 
					hx-target="#bgg-modal-body">
				➕ Add to My Collection
			</button>
			<button class="btn btn-secondary" onclick="closeModal()">Cancel</button>
		</div>`, game.ID)
	}

	fmt.Fprintf(w, `</div>`)
}

func renderBGGImportForm(w io.Writer, details *bgg.GameDetails, baseGames []database.Game) {
	fmt.Fprintf(w, `
	<form hx-post="/collection/import" 
	      hx-encoding="multipart/form-data" 
	      hx-target="#bgg-modal-body" 
	      hx-indicator="#import-spinner"
	      style="display: flex; flex-direction: column; gap: 1rem;">
		
		<input type="hidden" name="bgg_id" value="%d">
		<input type="hidden" name="bgg_url" value="%s">
		<input type="hidden" name="bgg_image_url" value="%s">
		<input type="hidden" name="rating" value="%.2f">

		<div style="display: flex; gap: 1rem; align-items: flex-start;">
			%s
			<div style="flex: 1;">
				<label class="form-label" style="font-weight: 600;">Game Title</label>
				<input type="text" name="name" class="form-input" value="%s" required style="width: 100%%;">
			</div>
		</div>

		<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 0.75rem;">
			<div>
				<label class="form-label" style="font-size: 0.85rem;">Min Players</label>
				<input type="number" name="min_players" class="form-input" value="%d" min="1" max="99" style="width: 100%%;">
			</div>
			<div>
				<label class="form-label" style="font-size: 0.85rem;">Max Players</label>
				<input type="number" name="max_players" class="form-input" value="%d" min="1" max="99" style="width: 100%%;">
			</div>
			<div>
				<label class="form-label" style="font-size: 0.85rem;">Best Players</label>
				<input type="text" name="best_players" class="form-input" value="%s" placeholder="e.g. 3-4" style="width: 100%%;">
			</div>
			<div>
				<label class="form-label" style="font-size: 0.85rem;">Weight / Complexity</label>
				<input type="number" name="complexity" class="form-input" value="%.2f" step="0.01" min="1.0" max="5.0" style="width: 100%%;">
			</div>
		</div>

		<div>
			<label class="form-label" style="font-weight: 600;">Is this an Expansion? (Optional)</label>
			<select name="parent_id" class="form-select" style="width: 100%%;">
				<option value="0">Standalone Game (No Parent)</option>`,
		details.ID,
		details.BggURL,
		details.Image,
		details.Rating,
		func() string {
			if details.Thumbnail != "" {
				return fmt.Sprintf(`<img src="%s" alt="Cover" style="width: 70px; height: 70px; object-fit: cover; border-radius: 8px; border: 1px solid var(--border-color);">`, details.Thumbnail)
			}
			return ""
		}(),
		templateEscape(details.Name),
		details.MinPlayers,
		details.MaxPlayers,
		templateEscape(details.BestPlayers),
		details.Complexity,
	)

	for _, bg := range baseGames {
		fmt.Fprintf(w, `<option value="%d">Expansion under: %s</option>`, bg.ID, templateEscape(bg.Name))
	}

	fmt.Fprintf(w, `
			</select>
			<span style="font-size: 0.8rem; color: var(--text-muted); display: block; margin-top: 4px;">
				Even standalone titles (e.g. Smash Up sets) can be grouped under a base game.
			</span>
		</div>

		<div>
			<label class="form-label" style="font-weight: 600;">PDF Rulebook <span style="color: var(--accent-primary);">*</span></label>
			<input type="file" name="pdf_file" accept=".pdf" class="form-input" required style="width: 100%%;">
			<span style="font-size: 0.8rem; color: var(--text-muted); display: block; margin-top: 4px;">
				Upload English rulebook (PDF format). Fast Web View will be applied automatically.
			</span>
		</div>

		<div>
			<label class="form-label" style="font-size: 0.85rem;">Custom Box Art (Optional)</label>
			<input type="file" name="image_file" accept=".jpg,.jpeg,.png,.webp" class="form-input" style="width: 100%%;">
			<span style="font-size: 0.8rem; color: var(--text-muted); display: block; margin-top: 4px;">
				If left empty, cover image will be downloaded automatically from BoardGameGeek.
			</span>
		</div>

		<div style="display: flex; align-items: center; justify-content: flex-end; gap: 0.75rem; margin-top: 0.5rem;">
			<button type="button" class="btn btn-secondary" onclick="closeModal()">Cancel</button>
			<button type="submit" class="btn btn-primary">
				🚀 Save & Add to Collection
			</button>
			<span id="import-spinner" class="htmx-indicator" style="color: var(--accent-primary); font-size: 0.9rem;">Saving...</span>
		</div>
	</form>`)
}

func renderReparentModal(w io.Writer, d ReparentModalData) {
	fmt.Fprintf(w, `
	<div class="modal-backdrop" onclick="if(event.target === this) closeModal()">
		<div class="modal-dialog" style="max-width: 520px;">
			<div class="modal-header">
				<h3 class="modal-title">Reparent Game / Manage Expansion</h3>
				<button type="button" class="modal-close" onclick="closeModal()">✕</button>
			</div>
			<div class="modal-body" id="reparent-modal-body">
				<div style="margin-bottom: 1rem;">
					<h4 style="margin: 0; font-size: 1.15rem; color: var(--text-primary);">%s</h4>
					<p style="margin: 4px 0 0 0; font-size: 0.85rem; color: var(--text-secondary);">
						Current Status: %s
					</p>
				</div>`,
		templateEscape(d.Game.Name),
		func() string {
			if d.CurrentParent != nil {
				return fmt.Sprintf("Expansion under <strong>%s</strong>", templateEscape(d.CurrentParent.Name))
			}
			return "<strong>Standalone Base Game</strong>"
		}(),
	)

	if d.SharedCount > 0 {
		fmt.Fprintf(w, `
		<div class="alert alert-warning" style="margin-bottom: 1rem; font-size: 0.9rem; line-height: 1.4;">
			⚠️ <strong>Shared Game Notice:</strong> This game is also in <strong>%d other user collection(s)</strong>%s.
			Moving this game changes its position across the entire shared library.
		</div>`,
			d.SharedCount,
			func() string {
				if len(d.SharedUsers) > 0 {
					return fmt.Sprintf(" (%s)", strings.Join(d.SharedUsers, ", "))
				}
				return ""
			}(),
		)
	}

	if d.IsRestricted {
		fmt.Fprintf(w, `
		<div class="alert alert-danger" style="margin-bottom: 1rem;">
			🔒 <strong>Move Restricted:</strong> Administrator policy prohibits non-admins from moving or reparenting games that belong to multiple users' collections. Please request an administrator to make this change.
		</div>
		<div style="display: flex; justify-content: flex-end;">
			<button class="btn btn-secondary" onclick="closeModal()">Close</button>
		</div>`)
	} else {
		fmt.Fprintf(w, `
		<form hx-post="/collection/games/%d/reparent" hx-target="#reparent-modal-body" style="display: flex; flex-direction: column; gap: 1rem;">
			<div>
				<label class="form-label" style="font-weight: 600;">Choose New Position</label>
				<select name="parent_id" class="form-select" style="width: 100%%;">
					<option value="0" %s>📌 Standalone Base Game (Root)</option>`,
			d.Game.ID,
			func() string {
				if d.Game.ParentID == nil {
					return "selected"
				}
				return ""
			}(),
		)

		for _, cand := range d.Candidates {
			isSelected := d.Game.ParentID != nil && *d.Game.ParentID == cand.ID
			selAttr := ""
			if isSelected {
				selAttr = "selected"
			}
			fmt.Fprintf(w, `<option value="%d" %s>🧩 Expansion under: %s</option>`, cand.ID, selAttr, templateEscape(cand.Name))
		}

		fmt.Fprintf(w, `
				</select>
			</div>
			<div style="display: flex; justify-content: flex-end; gap: 0.75rem; margin-top: 0.5rem;">
				<button type="button" class="btn btn-secondary" onclick="closeModal()">Cancel</button>
				<button type="submit" class="btn btn-primary">Apply Changes</button>
			</div>
		</form>`)
	}

	fmt.Fprintf(w, `
			</div>
		</div>
	</div>`)
}

// HandleCollectionRenameModal renders the modal to customize the collection's display name
func (h *Handler) HandleCollectionRenameModal(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r, h.cfg)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	currentName, _ := h.db.GetCollectionName(userID)
	if currentName == userID {
		currentName = ""
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
<div class="modal-backdrop" onclick="if(event.target === this) this.remove()">
	<div class="modal" style="max-width: 460px;">
		<div class="modal-header">
			<h3 class="modal-title" style="display: flex; align-items: center; gap: 0.5rem;">
				<span>✏️</span> Rename Collection
			</h3>
			<button type="button" class="modal-close" onclick="this.closest('.modal-backdrop').remove()">×</button>
		</div>
		<form hx-post="/collection/rename" 
		      hx-target="#modal-container"
		      style="display: flex; flex-direction: column; gap: 1.25rem;">
			<div>
				<label for="custom_name" style="display: block; font-weight: 600; margin-bottom: 0.5rem; font-size: 0.95rem; color: var(--text-primary);">
					Collection Name
				</label>
				<input type="text" 
				       id="custom_name" 
				       name="custom_name" 
				       value="%s" 
				       placeholder="%s" 
				       maxlength="30"
				       class="form-input" 
				       style="width: 100%%; font-size: 1rem; padding: 0.6rem 0.85rem;"
				       autofocus>
				<div style="display: flex; justify-content: space-between; margin-top: 0.4rem; font-size: 0.8rem; color: var(--text-muted);">
					<span>Leave blank to use default (<strong>%s</strong>).</span>
					<span>Max 30 chars</span>
				</div>
			</div>

			<div style="display: flex; justify-content: flex-end; gap: 0.75rem; margin-top: 0.5rem;">
				<button type="button" class="btn btn-secondary" onclick="this.closest('.modal-backdrop').remove()">
					Cancel
				</button>
				<button type="submit" class="btn btn-primary">
					Save Name
				</button>
			</div>
		</form>
	</div>
</div>`, templateEscape(currentName), templateEscape(userID), templateEscape(userID))
}

// HandleCollectionRename processes collection name update
func (h *Handler) HandleCollectionRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := middleware.CurrentUserID(r, h.cfg)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	name := strings.TrimSpace(r.FormValue("custom_name"))
	runes := []rune(name)
	if len(runes) > 30 {
		name = string(runes[:30])
	}

	if err := h.db.SetCollectionName(userID, name); err != nil {
		log.Printf("Error updating collection name for %s: %v", userID, err)
		http.Error(w, "Failed to update collection name", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/collection")
	w.WriteHeader(http.StatusOK)
}
