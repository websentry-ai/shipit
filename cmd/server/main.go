package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vigneshsubbiah/shipit/internal/api"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/config"
	"github.com/vigneshsubbiah/shipit/internal/db"
)

func main() {
	cfg := config.Load()

	// Validate encryption key
	if cfg.EncryptKey == "" {
		log.Println("WARNING: ENCRYPT_KEY not set. Generating a temporary key...")
		key, _ := auth.GenerateKey()
		cfg.EncryptKey = key
		log.Printf("Generated key (save this!): %s", key)
	}

	// Connect to database
	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	log.Println("Connected to database")

	// Create router
	router := api.NewRouter(database, cfg.EncryptKey)

	// Create server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Server starting on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	fmt.Println("Server exited")
}
