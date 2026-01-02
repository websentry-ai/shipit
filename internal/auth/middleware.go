package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/vigneshsubbiah/shipit/internal/db"
)

type contextKey string

const TokenContextKey contextKey = "api_token"

func Middleware(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, `{"error": "missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			apiToken, err := database.ValidateToken(r.Context(), token)
			if err != nil {
				http.Error(w, `{"error": "invalid token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), TokenContextKey, apiToken)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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

func GetToken(ctx context.Context) *db.APIToken {
	if token, ok := ctx.Value(TokenContextKey).(*db.APIToken); ok {
		return token
	}
	return nil
}
