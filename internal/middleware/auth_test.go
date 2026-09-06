package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/christian-klein/bgtags/internal/auth"
	"github.com/christian-klein/bgtags/internal/config"
)

func TestAuthMiddleware(t *testing.T) {
	cfg := &config.Config{
		OIDCEnabled:    true,
		OIDCAdminGroup: "bgtags-admins",
		OIDCUsersGroup: "bgtags-users",
		SessionSecret:  "test-secret-32b-secure-value-key",
	}

	adminSession := &auth.SessionData{
		Username:  "admin",
		Groups:    []string{"bgtags-admins"},
		CreatedAt: time.Now().UTC(),
	}

	userSession := &auth.SessionData{
		Username:  "user",
		Groups:    []string{"bgtags-users"},
		CreatedAt: time.Now().UTC(),
	}

	guestSession := &auth.SessionData{
		Username:  "guest",
		Groups:    []string{"other"},
		CreatedAt: time.Now().UTC(),
	}

	t.Run("RequireAdmin - Unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		rec := httptest.NewRecorder()

		handler := RequireAdmin(cfg, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler(rec, req)
		if rec.Code != http.StatusFound {
			t.Errorf("expected redirect 302, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login?return_to=%2Fadmin" {
			t.Errorf("expected redirect to /login, got %s", loc)
		}
	})

	t.Run("RequireAdmin - Regular User Forbidden", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, userSession))
		rec := httptest.NewRecorder()

		handler := RequireAdmin(cfg, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", rec.Code)
		}
	})

	t.Run("RequireAdmin - Admin Allowed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, adminSession))
		rec := httptest.NewRecorder()

		handler := RequireAdmin(cfg, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}
	})

	t.Run("RequireReader - Public Allowed When Group Empty", func(t *testing.T) {
		publicCfg := &config.Config{
			OIDCEnabled:    true,
			OIDCUsersGroup: "", // Public reader access
		}

		req := httptest.NewRequest("GET", "/stickers", nil)
		rec := httptest.NewRecorder()

		handler := RequireReader(publicCfg, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for public access, got %d", rec.Code)
		}
	})

	t.Run("RequireReader - Restricted When Group Configured", func(t *testing.T) {
		// 1. Unauthenticated -> redirect to login
		reqUnauth := httptest.NewRequest("GET", "/stickers", nil)
		recUnauth := httptest.NewRecorder()

		handler := RequireReader(cfg, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler(recUnauth, reqUnauth)
		if recUnauth.Code != http.StatusFound {
			t.Errorf("expected 302 redirect for reader without auth, got %d", recUnauth.Code)
		}

		// 2. Authenticated user in bgtags-users -> OK
		reqUser := httptest.NewRequest("GET", "/stickers", nil)
		reqUser = reqUser.WithContext(context.WithValue(reqUser.Context(), UserContextKey, userSession))
		recUser := httptest.NewRecorder()
		handler(recUser, reqUser)
		if recUser.Code != http.StatusOK {
			t.Errorf("expected 200 OK for user with bgtags-users, got %d", recUser.Code)
		}

		// 3. Authenticated admin -> OK
		reqAdmin := httptest.NewRequest("GET", "/stickers", nil)
		reqAdmin = reqAdmin.WithContext(context.WithValue(reqAdmin.Context(), UserContextKey, adminSession))
		recAdmin := httptest.NewRecorder()
		handler(recAdmin, reqAdmin)
		if recAdmin.Code != http.StatusOK {
			t.Errorf("expected 200 OK for admin, got %d", recAdmin.Code)
		}

		// 4. Authenticated guest not in group -> 403
		reqGuest := httptest.NewRequest("GET", "/stickers", nil)
		reqGuest = reqGuest.WithContext(context.WithValue(reqGuest.Context(), UserContextKey, guestSession))
		recGuest := httptest.NewRecorder()
		handler(recGuest, reqGuest)
		if recGuest.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for user not in users or admin group, got %d", recGuest.Code)
		}
	})

	t.Run("RequireReader - Wildcard Group Allows Any Authenticated User", func(t *testing.T) {
		wildcardCfg := &config.Config{
			OIDCEnabled:    true,
			OIDCUsersGroup: "*",
		}

		handler := RequireReader(wildcardCfg, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Unauthenticated -> 302 to login
		reqUnauth := httptest.NewRequest("GET", "/stickers", nil)
		recUnauth := httptest.NewRecorder()
		handler(recUnauth, reqUnauth)
		if recUnauth.Code != http.StatusFound {
			t.Errorf("expected 302 redirect for reader without auth, got %d", recUnauth.Code)
		}

		// Any authenticated user (even guest with no groups) -> 200 OK
		reqGuest := httptest.NewRequest("GET", "/stickers", nil)
		reqGuest = reqGuest.WithContext(context.WithValue(reqGuest.Context(), UserContextKey, guestSession))
		recGuest := httptest.NewRecorder()
		handler(recGuest, reqGuest)
		if recGuest.Code != http.StatusOK {
			t.Errorf("expected 200 OK for any authenticated user when group is *, got %d", recGuest.Code)
		}
	})
}

