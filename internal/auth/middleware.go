package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/vigneshsubbiah/shipit/internal/db"
)

type contextKey string

const (
	TokenContextKey   contextKey = "api_token"
	UserContextKey    contextKey = "user"
	SessionContextKey contextKey = "session"
)

// Middleware creates authentication middleware that supports:
// 1. Session cookies (for web dashboard)
// 2. User tokens (Bearer token, user-generated for CLI)
// 3. Legacy API tokens (for backwards compatibility)
func Middleware(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Try session cookie first (for web dashboard)
			if session, user := validateSessionCookie(r, database); session != nil && user != nil {
				ctx = context.WithValue(ctx, SessionContextKey, session)
				ctx = context.WithValue(ctx, UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Try Bearer token (user token or legacy API token)
			token := extractToken(r)
			if token != "" {
				// Try user token first
				if userToken, user := validateUserToken(r.Context(), database, token); userToken != nil && user != nil {
					ctx = context.WithValue(ctx, TokenContextKey, userToken)
					ctx = context.WithValue(ctx, UserContextKey, user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				// Fall back to legacy API token
				if apiToken, err := database.ValidateToken(r.Context(), token); err == nil {
					ctx = context.WithValue(ctx, TokenContextKey, apiToken)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// No valid authentication
			http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		})
	}
}

// validateSessionCookie checks for a valid session cookie
func validateSessionCookie(r *http.Request, database *db.DB) (*db.Session, *db.User) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, nil
	}

	sessionHash := hashString(cookie.Value)
	session, err := database.ValidateSession(r.Context(), sessionHash)
	if err != nil {
		return nil, nil
	}

	user, err := database.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		return nil, nil
	}

	return session, user
}

// validateUserToken validates a user-generated API token
func validateUserToken(ctx context.Context, database *db.DB, token string) (*db.UserToken, *db.User) {
	tokenHash := hashString(token)
	userToken, err := database.ValidateUserToken(ctx, tokenHash)
	if err != nil {
		return nil, nil
	}

	user, err := database.GetUserByID(ctx, userToken.UserID)
	if err != nil {
		return nil, nil
	}

	return userToken, user
}

func extractToken(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Check X-API-Token header (alternative)
	if token := r.Header.Get("X-API-Token"); token != "" {
		return token
	}

	return ""
}

// GetToken returns the legacy API token from context (for backwards compatibility)
func GetToken(ctx context.Context) *db.APIToken {
	if token, ok := ctx.Value(TokenContextKey).(*db.APIToken); ok {
		return token
	}
	return nil
}

// GetUser returns the authenticated user from context
func GetUser(ctx context.Context) *db.User {
	if user, ok := ctx.Value(UserContextKey).(*db.User); ok {
		return user
	}
	return nil
}

// GetSession returns the session from context
func GetSession(ctx context.Context) *db.Session {
	if session, ok := ctx.Value(SessionContextKey).(*db.Session); ok {
		return session
	}
	return nil
}

// GetUserToken returns the user token from context
func GetUserToken(ctx context.Context) *db.UserToken {
	if token, ok := ctx.Value(TokenContextKey).(*db.UserToken); ok {
		return token
	}
	return nil
}

// IsAuthenticated returns true if the request is authenticated
func IsAuthenticated(ctx context.Context) bool {
	return GetUser(ctx) != nil || GetToken(ctx) != nil
}
