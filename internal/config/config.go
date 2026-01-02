package config

import (
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	EncryptKey  string // 32-byte key for AES-256 encryption of kubeconfigs
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8090"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://shipit:shipit@localhost:5433/shipit?sslmode=disable"),
		EncryptKey:  getEnv("ENCRYPT_KEY", ""), // Must be set in production
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
