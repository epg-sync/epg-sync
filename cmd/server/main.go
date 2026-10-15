package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/epg-sync/epgsync/internal/app"
	"github.com/epg-sync/epgsync/internal/config"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/all"
	"github.com/epg-sync/epgsync/pkg/logger"
)

func main() {
	configs := config.MustLoad("config/config.yaml")

	err := logger.Init(&configs.Logger)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	application, err := app.New(configs)
	if err != nil {
		logger.Fatal("Failed to create application", logger.Err(err))
	}

	if err := application.Start(); err != nil {
		logger.Fatal("Failed to start application", logger.Err(err))
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	application.Stop()

	logger.Info("Server stopped")
}
