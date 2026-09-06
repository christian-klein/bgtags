package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCService struct {
	issuerURL    string
	clientID     string
	clientSecret string
	redirectURI  string
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	enabled      bool
}

type TokenClaims struct {
	Subject           string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email"`
	Name              string   `json:"name"`
	Groups            []string `json:"groups"`
}

func NewOIDCService(ctx context.Context, issuerURL, clientID, clientSecret, redirectURI string, enabled bool) (*OIDCService, error) {
	service := &OIDCService{
		issuerURL:    issuerURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
		enabled:      enabled,
	}

	if !enabled || clientID == "" {
		return service, nil
	}

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("initializing oidc provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
	})

	oauth2Config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
	}

	service.provider = provider
	service.verifier = verifier
	service.oauth2Config = oauth2Config

	return service, nil
}

func (s *OIDCService) IsEnabled() bool {
	return s != nil && s.enabled && s.provider != nil
}

// GeneratePKCE creates code_verifier and code_challenge (S256)
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, nil
}

// GenerateRandomState generates a secure random state string
func GenerateRandomState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// AuthURL builds the Authentik authorization redirect URL with PKCE
func (s *OIDCService) AuthURL(state, codeChallenge, redirectURI string) string {
	cfg := s.oauth2Config
	if redirectURI != "" {
		cfg.RedirectURL = redirectURI
	}

	return cfg.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// ExchangeCode exchanges code with Authentik and extracts user claims
func (s *OIDCService) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI string) (*TokenClaims, error) {
	if !s.IsEnabled() {
		return nil, errors.New("oidc service is not enabled or configured")
	}

	cfg := s.oauth2Config
	if redirectURI != "" {
		cfg.RedirectURL = redirectURI
	}

	token, err := cfg.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("oauth2 token exchange failed: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("missing id_token in token response")
	}

	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verifying id_token: %w", err)
	}

	var claims TokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parsing id_token claims: %w", err)
	}

	// Also query UserInfo endpoint if groups aren't directly in id_token
	if len(claims.Groups) == 0 && s.provider != nil {
		userInfo, err := s.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err == nil {
			var extraClaims struct {
				Groups []string `json:"groups"`
			}
			if err := userInfo.Claims(&extraClaims); err == nil && len(extraClaims.Groups) > 0 {
				claims.Groups = extraClaims.Groups
			}
		}
	}

	if claims.PreferredUsername == "" {
		if claims.Name != "" {
			claims.PreferredUsername = claims.Name
		} else if claims.Email != "" {
			claims.PreferredUsername = strings.Split(claims.Email, "@")[0]
		} else {
			claims.PreferredUsername = "User"
		}
	}

	return &claims, nil
}
