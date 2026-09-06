package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionSignAndVerify(t *testing.T) {
	secret := "test-secret-key-that-is-secure-32b"
	session := &SessionData{
		Username:  "adminuser",
		Name:      "Admin User",
		Email:     "admin@example.com",
		Groups:    []string{"bgtags-admins", "users"},
		CreatedAt: time.Now().UTC(),
	}

	token, err := SignAndEncode(session, secret)
	if err != nil {
		t.Fatalf("SignAndEncode failed: %v", err)
	}

	decoded, err := DecodeAndVerify(token, secret)
	if err != nil {
		t.Fatalf("DecodeAndVerify failed: %v", err)
	}

	if decoded.Username != session.Username {
		t.Errorf("expected username %s, got %s", session.Username, decoded.Username)
	}

	if !decoded.HasGroup("bgtags-admins") {
		t.Errorf("expected group bgtags-admins")
	}

	if decoded.HasGroup("non-existent") {
		t.Errorf("unexpected group found")
	}

	// Test tampering
	tamperedToken := token + "tamper"
	if _, err := DecodeAndVerify(tamperedToken, secret); err == nil {
		t.Errorf("expected error for tampered token")
	}

	// Test invalid secret
	if _, err := DecodeAndVerify(token, "wrong-secret"); err == nil {
		t.Errorf("expected error for wrong secret")
	}
}

func TestSessionCookies(t *testing.T) {
	secret := "test-secret-key-that-is-secure-32b"
	session := &SessionData{
		Username:  "regularuser",
		Name:      "Regular User",
		Email:     "user@example.com",
		Groups:    []string{"users"},
		CreatedAt: time.Now().UTC(),
	}

	rec := httptest.NewRecorder()
	err := SetSessionCookie(rec, session, secret, false)
	if err != nil {
		t.Fatalf("SetSessionCookie failed: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected cookie to be set")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])

	retrieved := GetSessionFromRequest(req, secret)
	if retrieved == nil {
		t.Fatalf("failed to retrieve session from cookie")
	}
	if retrieved.Username != "regularuser" {
		t.Errorf("expected regularuser, got %s", retrieved.Username)
	}

	// Test clear cookie
	recClear := httptest.NewRecorder()
	ClearSessionCookie(recClear)
	clearCookies := recClear.Result().Cookies()
	if len(clearCookies) == 0 || clearCookies[0].MaxAge != -1 {
		t.Errorf("expected cookie with MaxAge -1")
	}
}
