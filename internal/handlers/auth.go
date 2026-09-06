package handlers

import (
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/christian-klein/bgtags/internal/auth"
)

const (
	cookieOAuthState    = "bgtags_oauth_state"
	cookieOAuthVerifier = "bgtags_oauth_verifier"
	cookieOAuthReturnTo = "bgtags_oauth_return_to"
)

func (h *Handler) getOAuthRedirectURI(r *http.Request) string {
	if h.cfg.OIDCRedirectURL != "" {
		return h.cfg.OIDCRedirectURL
	}
	return h.getBaseURL(r) + "/auth/callback"
}

func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.OIDCEnabled || h.oidc == nil || !h.oidc.IsEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	verifier, challenge, err := auth.GeneratePKCE()
	if err != nil {
		log.Printf("Error generating PKCE: %v", err)
		http.Error(w, "Authentication initialization error", http.StatusInternalServerError)
		return
	}

	state := auth.GenerateRandomState()
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" || !isValidReturnTo(returnTo) {
		returnTo = "/"
	}

	isTLS := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	setTempCookie(w, cookieOAuthState, state, 300, isTLS)
	setTempCookie(w, cookieOAuthVerifier, verifier, 300, isTLS)
	setTempCookie(w, cookieOAuthReturnTo, returnTo, 300, isTLS)

	redirectURI := h.getOAuthRedirectURI(r)
	authURL := h.oidc.AuthURL(state, challenge, redirectURI)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) HandleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.OIDCEnabled || h.oidc == nil || !h.oidc.IsEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("OIDC callback error: %s: %s", errParam, errDesc)
		http.Error(w, "Authentication rejected: "+errDesc, http.StatusUnauthorized)
		return
	}

	stateQuery := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie(cookieOAuthState)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != stateQuery {
		log.Printf("OIDC state mismatch: query=%s cookie=%v", stateQuery, stateCookie)
		http.Error(w, "Invalid or expired login session (state mismatch)", http.StatusBadRequest)
		return
	}

	verifierCookie, err := r.Cookie(cookieOAuthVerifier)
	if err != nil || verifierCookie.Value == "" {
		http.Error(w, "Missing PKCE code verifier", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	redirectURI := h.getOAuthRedirectURI(r)
	claims, err := h.oidc.ExchangeCode(r.Context(), code, verifierCookie.Value, redirectURI)
	if err != nil {
		log.Printf("OIDC code exchange failed: %v", err)
		http.Error(w, "Failed to authenticate with identity provider", http.StatusInternalServerError)
		return
	}

	session := &auth.SessionData{
		Username:  claims.PreferredUsername,
		Name:      claims.Name,
		Email:     claims.Email,
		Groups:    claims.Groups,
		CreatedAt: time.Now().UTC(),
	}

	isTLS := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	if err := auth.SetSessionCookie(w, session, h.cfg.SessionSecret, isTLS); err != nil {
		log.Printf("Failed to set session cookie: %v", err)
		http.Error(w, "Failed to establish user session", http.StatusInternalServerError)
		return
	}

	returnTo := "/"
	if c, err := r.Cookie(cookieOAuthReturnTo); err == nil && c.Value != "" && isValidReturnTo(c.Value) {
		returnTo = c.Value
	}

	clearTempCookie(w, cookieOAuthState)
	clearTempCookie(w, cookieOAuthVerifier)
	clearTempCookie(w, cookieOAuthReturnTo)

	log.Printf("[bgtags] User logged in: %s (groups: %v)", session.Username, session.Groups)
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func setTempCookie(w http.ResponseWriter, name, val string, maxAge int, isTLS bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    val,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isTLS,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearTempCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Expires:  time.Unix(0, 0),
	})
}

func isValidReturnTo(p string) bool {
	u, err := url.Parse(p)
	if err != nil {
		return false
	}
	// Only relative paths to avoid open redirects
	return u.Scheme == "" && u.Host == "" && len(u.Path) > 0 && u.Path[0] == '/'
}
