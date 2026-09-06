package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const SessionCookieName = "bgtags_session"

type SessionData struct {
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Groups    []string  `json:"groups"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *SessionData) HasGroup(target string) bool {
	if s == nil || target == "" {
		return false
	}
	for _, g := range s.Groups {
		if strings.EqualFold(g, target) {
			return true
		}
	}
	return false
}

// SignAndEncode creates a base64-encoded payload with HMAC-SHA256 signature
func SignAndEncode(data *SessionData, secret string) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(bytes)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payloadB64 + "." + sigB64, nil
}

// DecodeAndVerify validates the HMAC-SHA256 signature and decodes the payload
func DecodeAndVerify(token, secret string) (*SessionData, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid session token format")
	}

	payloadB64 := parts[0]
	sigB64 := parts[1]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	expectedSig := mac.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, errors.New("invalid signature encoding")
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, errors.New("invalid session signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, errors.New("invalid payload encoding")
	}

	var data SessionData
	if err := json.Unmarshal(payloadBytes, &data); err != nil {
		return nil, errors.New("failed to parse session payload")
	}

	// 7 days expiration
	if time.Since(data.CreatedAt) > 7*24*time.Hour {
		return nil, errors.New("session expired")
	}

	return &data, nil
}

func SetSessionCookie(w http.ResponseWriter, data *SessionData, secret string, isSecure bool) error {
	token, err := SignAndEncode(data, secret)
	if err != nil {
		return err
	}

	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	}
	http.SetCookie(w, cookie)
	return nil
}

func GetSessionFromRequest(r *http.Request, secret string) *SessionData {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	session, err := DecodeAndVerify(cookie.Value, secret)
	if err != nil {
		return nil
	}
	return session
}

func ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	}
	http.SetCookie(w, cookie)
}
