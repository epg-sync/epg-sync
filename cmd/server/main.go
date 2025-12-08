package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/epg-sync/epgsync/internal/app"
	"github.com/epg-sync/epgsync/internal/config"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/bfgd"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/btzx"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/cctv"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/daxiang"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/fengshow"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/hainan"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/hebei"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/iqilu"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/jsp"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/jstv"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/kknews"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/migu"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/shanxi"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/suzhou"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/sxrtv"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/xiamen"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/ysp"
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
