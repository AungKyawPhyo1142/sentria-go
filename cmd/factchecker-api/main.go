package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AungKyawPhyo1142/sentria-go/internal/api"
	"github.com/AungKyawPhyo1142/sentria-go/internal/config"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Starting server on port %d", cfg.ServerPort)

	router := api.SetupRouter(cfg)

	// TODO: init jobQueue consumer

	serverAddr := fmt.Sprintf(":%d", cfg.ServerPort)
	go func() {
		if err := router.Run(serverAddr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
	log.Printf("Gin server started on port %v", serverAddr)

	// graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // block until signal is received
	log.Println("Shutting down server...")

	// TODO: close jobQueue consumer
	log.Println("Server stopped")

}
