package middleware

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/christian-klein/bgtags/internal/auth"
	"github.com/christian-klein/bgtags/internal/config"
)

type contextKey string

const (
	UserContextKey contextKey = "user_session"
)

// WithUserContext populates the request context with session data if present
func WithUserContext(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := auth.GetSessionFromRequest(r, cfg.SessionSecret)
			if session != nil {
				ctx := context.WithValue(r.Context(), UserContextKey, session)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserFromContext retrieves the current session from request context
func UserFromContext(ctx context.Context) *auth.SessionData {
	if val, ok := ctx.Value(UserContextKey).(*auth.SessionData); ok {
		return val
	}
	return nil
}

// IsAdmin checks if the current user has admin privileges
func IsAdmin(r *http.Request, cfg *config.Config) bool {
	if !cfg.OIDCEnabled {
		return true // In local dev mode without OIDC, treat as admin
	}
	session := UserFromContext(r.Context())
	if session == nil {
		return false
	}
	if cfg.OIDCAdminGroup == "" {
		return true
	}
	return session.HasGroup(cfg.OIDCAdminGroup)
}

// IsAuthenticated checks if the current user has an active session
func IsAuthenticated(r *http.Request, cfg *config.Config) bool {
	if !cfg.OIDCEnabled {
		return true
	}
	return UserFromContext(r.Context()) != nil
}

// RequireAdmin middleware blocks requests unless the user is an admin
func RequireAdmin(cfg *config.Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.OIDCEnabled {
			next(w, r)
			return
		}

		session := UserFromContext(r.Context())
		if session == nil {
			returnTo := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/login?return_to="+returnTo, http.StatusFound)
			return
		}

		if cfg.OIDCAdminGroup != "" && !session.HasGroup(cfg.OIDCAdminGroup) {
			http.Error(w, "Forbidden: Administrator role required", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// RequireReader middleware enforces authentication/group for reading if OIDC_USERS_GROUP is set
func RequireReader(cfg *config.Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If OIDC is disabled or no users group is configured, public access is permitted
		if !cfg.OIDCEnabled || cfg.OIDCUsersGroup == "" {
			next(w, r)
			return
		}

		session := UserFromContext(r.Context())
		if session == nil {
			returnTo := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/login?return_to="+returnTo, http.StatusFound)
			return
		}

		// If wildcard "*" or "any", any authenticated user has reader access
		if cfg.OIDCUsersGroup == "*" || strings.EqualFold(cfg.OIDCUsersGroup, "any") || cfg.OIDCUsersGroup == "@authenticated" {
			next(w, r)
			return
		}

		// User must belong to users group or admin group
		hasUserGroup := session.HasGroup(cfg.OIDCUsersGroup)
		hasAdminGroup := cfg.OIDCAdminGroup != "" && session.HasGroup(cfg.OIDCAdminGroup)

		if !hasUserGroup && !hasAdminGroup {
			http.Error(w, "Forbidden: Access restricted to authorized users", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}
