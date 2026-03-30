package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port        string
	DatabaseURL string
	EncryptKey  string // 32-byte key for AES-256 encryption of kubeconfigs

	// Google OAuth (SSO)
	GoogleClientID     string
	GoogleClientSecret string
	OAuthRedirectURL   string
	AllowedEmailDomain string // e.g., "unboundsecurity.ai"

	// Session configuration
	SessionSecret string // Secret for signing session tokens
	SessionMaxAge int    // Session duration in seconds (default: 86400 = 24h)
	CookieSecure  bool   // Set Secure flag on cookies (true in prod)
	CookieDomain  string // Cookie domain (e.g., "shipit.unboundsec.dev")

	// Default app URL configuration
	AppBaseDomain string // e.g., "apps.shipit.unboundsec.dev" - apps get URLs like <name>.apps.shipit.unboundsec.dev
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8090"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://shipit:shipit@localhost:5433/shipit?sslmode=disable"),
		EncryptKey:  getEnv("ENCRYPT_KEY", ""), // Must be set in production

		// Google OAuth
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		OAuthRedirectURL:   getEnv("OAUTH_REDIRECT_URL", "http://localhost:8090/auth/callback"),
		AllowedEmailDomain: getEnv("ALLOWED_EMAIL_DOMAIN", ""),

		// Session
		SessionSecret: getEnv("SESSION_SECRET", ""),
		SessionMaxAge: getEnvInt("SESSION_MAX_AGE", 86400),
		CookieSecure:  getEnvBool("COOKIE_SECURE", false),
		CookieDomain:  getEnv("COOKIE_DOMAIN", ""),

		// App URLs
		AppBaseDomain: getEnv("APP_BASE_DOMAIN", ""), // e.g., "apps.shipit.unboundsec.dev"
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return fallback
}
