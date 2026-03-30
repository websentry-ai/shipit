package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/vigneshsubbiah/shipit/internal/config"
	"github.com/vigneshsubbiah/shipit/internal/db"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	SessionCookieName = "shipit_session"
	StateCookieName   = "oauth_state"
)

// GoogleUserInfo represents the user info returned by Google
type GoogleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// OAuthHandler handles Google OAuth authentication
type OAuthHandler struct {
	config   *config.Config
	database *db.DB
	oauth    *oauth2.Config
}

// NewOAuthHandler creates a new OAuth handler
func NewOAuthHandler(cfg *config.Config, database *db.DB) *OAuthHandler {
	oauthConfig := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.OAuthRedirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}

	return &OAuthHandler{
		config:   cfg,
		database: database,
		oauth:    oauthConfig,
	}
}

// HandleLogin redirects to Google OAuth
func (h *OAuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// Generate state for CSRF protection
	state, err := generateRandomString(32)
	if err != nil {
		http.Error(w, "Failed to generate state", http.StatusInternalServerError)
		return
	}

	// Store state in cookie
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		Secure:   h.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to Google
	url := h.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// HandleCallback processes the OAuth callback from Google
func (h *OAuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify state
	stateCookie, err := r.Cookie(StateCookieName)
	if err != nil {
		http.Error(w, "Missing state cookie", http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.config.CookieSecure,
	})

	// Check for errors from Google
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		log.Printf("OAuth error: %s", errMsg)
		http.Redirect(w, r, "/login?error="+errMsg, http.StatusTemporaryRedirect)
		return
	}

	// Exchange code for token
	code := r.URL.Query().Get("code")
	token, err := h.oauth.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("Failed to exchange token: %v", err)
		http.Redirect(w, r, "/login?error=exchange_failed", http.StatusTemporaryRedirect)
		return
	}

	// Get user info from Google
	userInfo, err := h.getUserInfo(r.Context(), token)
	if err != nil {
		log.Printf("Failed to get user info: %v", err)
		http.Redirect(w, r, "/login?error=userinfo_failed", http.StatusTemporaryRedirect)
		return
	}

	// Validate email domain
	if h.config.AllowedEmailDomain != "" {
		if !strings.HasSuffix(userInfo.Email, "@"+h.config.AllowedEmailDomain) {
			log.Printf("Unauthorized email domain: %s", userInfo.Email)
			http.Redirect(w, r, "/login?error=unauthorized_domain", http.StatusTemporaryRedirect)
			return
		}
	}

	// Find or create user
	user, err := h.findOrCreateUser(r.Context(), userInfo)
	if err != nil {
		log.Printf("Failed to find/create user: %v", err)
		http.Redirect(w, r, "/login?error=user_failed", http.StatusTemporaryRedirect)
		return
	}

	// Create session
	sessionToken, err := generateRandomString(32)
	if err != nil {
		log.Printf("Failed to generate session token: %v", err)
		http.Redirect(w, r, "/login?error=session_failed", http.StatusTemporaryRedirect)
		return
	}

	sessionHash := hashString(sessionToken)
	expiresAt := time.Now().Add(time.Duration(h.config.SessionMaxAge) * time.Second)
	userAgent := r.Header.Get("User-Agent")
	ipAddress := getClientIP(r)

	_, err = h.database.CreateSession(r.Context(), user.ID, sessionHash, expiresAt, &userAgent, &ipAddress)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		http.Redirect(w, r, "/login?error=session_failed", http.StatusTemporaryRedirect)
		return
	}

	// Update last login
	_ = h.database.UpdateUserLastLogin(r.Context(), user.ID)

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   h.config.SessionMaxAge,
		HttpOnly: true,
		Secure:   h.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Domain:   h.config.CookieDomain,
	})

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// HandleLogout clears the session
func (h *OAuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Get session cookie
	cookie, err := r.Cookie(SessionCookieName)
	if err == nil && cookie.Value != "" {
		// Delete session from database
		sessionHash := hashString(cookie.Value)
		_ = h.database.DeleteSession(r.Context(), sessionHash)
	}

	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Domain:   h.config.CookieDomain,
	})

	// Return success for API calls, redirect for browser
	if r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success": true}`))
	} else {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
	}
}

// getUserInfo fetches user info from Google
func (h *OAuthHandler) getUserInfo(ctx context.Context, token *oauth2.Token) (*GoogleUserInfo, error) {
	client := h.oauth.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status %d", resp.StatusCode)
	}

	var userInfo GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// findOrCreateUser finds existing user or creates new one
func (h *OAuthHandler) findOrCreateUser(ctx context.Context, info *GoogleUserInfo) (*db.User, error) {
	// Try to find by Google ID first
	user, err := h.database.GetUserByGoogleID(ctx, info.ID)
	if err == nil {
		// Update profile info
		_ = h.database.UpdateUserProfile(ctx, user.ID, info.Name, info.Picture)
		return user, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// Try to find by email
	user, err = h.database.GetUserByEmail(ctx, info.Email)
	if err == nil {
		// Link Google ID to existing user and update profile
		_ = h.database.UpdateUserProfile(ctx, user.ID, info.Name, info.Picture)
		return user, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// Create new user
	return h.database.CreateUser(ctx, info.Email, info.Name, info.Picture, info.ID)
}

// Helper functions

func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for load balancers)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return strings.Split(r.RemoteAddr, ":")[0]
}
