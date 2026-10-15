package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/epg-sync/epgsync/internal/app"
	"github.com/epg-sync/epgsync/internal/config"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/all"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/joho/godotenv"
)

func main() {
	// 本地开发时从项目根目录的 .env 读取 APP_ENV、POSTGRES_PASSWORD 等，已存在的环境变量不会被覆盖
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("Failed to load .env: %v", err)
	}

	configs := config.MustLoad()

	err := logger.Init(&configs.Logger)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	logger.Info("Configuration loaded",
		logger.String("env", string(configs.Env)),
		logger.Strings("config_files", configs.LoadedFiles),
		logger.String("database", configs.Database.Target()),
	)

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
