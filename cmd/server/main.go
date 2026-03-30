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
	"github.com/vigneshsubbiah/shipit/internal/porter"
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

	// Create Porter discovery service
	porterDiscovery := porter.NewDiscoveryService(database)

	// Register existing clusters with Porter discovery
	clusters, err := database.ListAllClustersWithKubeconfig(context.Background())
	if err != nil {
		log.Printf("Warning: Failed to load clusters for Porter discovery: %v", err)
	} else {
		for _, cluster := range clusters {
			kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, cfg.EncryptKey)
			if err != nil {
				log.Printf("Warning: Failed to decrypt kubeconfig for cluster %s: %v", cluster.Name, err)
				continue
			}
			porterDiscovery.RegisterCluster(cluster.ID, kubeconfig)
		}
		log.Printf("Registered %d clusters with Porter discovery", len(clusters))
	}

	// Start Porter discovery in background
	discoveryCtx, discoveryCancel := context.WithCancel(context.Background())
	go porterDiscovery.Start(discoveryCtx)

	// Create router with Porter discovery service
	router := api.NewRouter(database, cfg, porterDiscovery)

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

	// Stop Porter discovery service
	discoveryCancel()
	porterDiscovery.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	fmt.Println("Server exited")
}
