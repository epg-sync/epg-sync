package main

import (
	"errors"
	"fmt"
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

const logHubSize = 4096

func main() {
	// 本地开发时从项目根目录的 .env 读取 APP_ENV、POSTGRES_PASSWORD 等，已存在的环境变量不会被覆盖
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("Failed to load .env: %v", err)
	}

	configs := config.MustLoad()

	// 子命令：重置管理员密码，见 runCommand
	if len(os.Args) > 1 {
		if err := logger.Init(&configs.Logger); err != nil {
			log.Fatalf("Failed to initialize logger: %v", err)
		}
		os.Exit(runCommand(configs, os.Args[1:]))
	}

	// 日志除了输出到 stdout，还经由 logHub 写入数据库，供管理后台查看。
	// 连上数据库之前的日志先留在缓冲区里
	logHub := logger.NewHub(logHubSize)
	err := logger.Init(&configs.Logger, logHub.Core(logger.ParseLevel(configs.Logger.Level)))
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	logger.Info("Configuration loaded",
		logger.String("env", string(configs.Env)),
		logger.Strings("config_files", configs.LoadedFiles),
		logger.String("database", configs.Database.Target()),
	)

	application, err := app.New(configs, logHub)
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

func runCommand(cfg *config.AppConfig, args []string) int {
	switch args[0] {
	case "reset-admin-password":
		// 新密码可以作为参数传入，不传则随机生成
		var password string
		if len(args) > 1 {
			password = args[1]
		}
		if err := app.ResetAdminPassword(cfg, password); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n\nUsage:\n  epg-sync                                   start the server\n  epg-sync reset-admin-password [password]   reset the admin password (random if omitted)\n", args[0])
		return 2
	}
}
