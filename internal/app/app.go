package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/epg-sync/epgsync/internal/api/http/handler"
	"github.com/epg-sync/epgsync/internal/api/http/router"
	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/config"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/internal/service"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type App struct {
	cfg           *config.AppConfig
	logHub        *logger.Hub
	db            *gorm.DB
	cache         cache.Cache
	repos         *Repositories
	services      *Services
	providerChain *provider.Chain
	server        *http.Server
}

type Repositories struct {
	Channel         repository.ChannelRepository
	Program         repository.ProgramRepository
	ChannelMappings repository.ChannelMappingsRepository
	Timezone        repository.TimezoneRepository
	User            repository.UserRepository
	Log             repository.LogRepository
}

type Services struct {
	EPG            *service.EPGService
	Channel        *service.ChannelService
	ChannelMapping *service.ChannelMappingService
	Scheduler      *service.SchedulerService
	User           *service.UserService
	Log            *service.LogService
}

// New 创建应用。logHub 是 logger 的日志缓冲，连上数据库后开始把其中的日志写入数据库供管理后台查看
func New(cfg *config.AppConfig, logHub *logger.Hub) (*App, error) {
	app := &App{
		cfg:    cfg,
		logHub: logHub,
	}

	if err := app.initialize(); err != nil {
		return nil, err
	}

	return app, nil
}

func (app *App) initialize() error {

	if err := app.initializeDatabase(); err != nil {
		return fmt.Errorf("init database: %w", err)
	}

	if err := app.initializeCache(); err != nil {
		return fmt.Errorf("init cache: %w", err)
	}

	if err := app.initializeRepositories(); err != nil {
		return fmt.Errorf("init repositories: %w", err)
	}

	app.providerChain = provider.GlobalFactory().CreateChain(app.cfg.Providers, app.cache)

	if err := app.initializeServices(); err != nil {
		return fmt.Errorf("init provider chain: %w", err)
	}

	// 启动以来的日志都在 logHub 的缓冲区里，开始写入后会一并保存
	app.services.Log.Start()

	if err := app.autoMapProviderChannels(); err != nil {
		return fmt.Errorf("auto map provider channels: %w", err)
	}

	return nil
}

func (app *App) Start() error {

	go func() {
		logger.Info("Initializing HTTP server...",
			logger.Int("port", app.cfg.Server.Port),
		)
		channelHandler := handler.NewChannelHandler(app.services.Channel)
		epgHandler := handler.NewEPGHandler(app.services.EPG)
		schedulerHandler := handler.NewSchedulerHandler(app.services.Scheduler)
		authHandler := handler.NewAuthHandler(app.services.User)
		logHandler := handler.NewLogHandler(app.services.Log)

		if app.cfg.Server.Mode == "release" {
			gin.SetMode(gin.ReleaseMode)
		}

		r := router.SetupRouter(
			app.cfg,
			channelHandler,
			epgHandler,
			schedulerHandler,
			authHandler,
			logHandler,
		)

		app.services.Scheduler.Start()

		app.server = &http.Server{
			Addr:         fmt.Sprintf(":%d", app.cfg.Server.Port),
			Handler:      r,
			ReadTimeout:  time.Duration(app.cfg.Server.Timeout) * time.Second,
			WriteTimeout: time.Duration(app.cfg.Server.Timeout) * time.Second,
		}

		go func() {
			if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("HTTP server error", logger.Err(err))
			}
		}()

		logger.Info("HTTP server initialized successfully")

	}()

	return nil
}

func (app *App) Stop() {

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.server.Shutdown(ctx); err != nil {
		logger.Error("Failed to shutdown HTTP server", logger.Err(err))
	}

	// 先把缓冲区里的日志写完，再关闭数据库
	if app.services != nil && app.services.Log != nil {
		app.services.Log.Stop()
	}

	if app.db != nil {
		sqlDB, err := app.db.DB()
		if err != nil {
			logger.Error("Failed to get sql.DB from gorm.DB", logger.Err(err))
			return
		}
		if err := sqlDB.Close(); err != nil {
			logger.Error("Failed to close database", logger.Err(err))
		}
	}

	if app.cache != nil {
		if err := app.cache.Close(); err != nil {
			logger.Error("Failed to close cache", logger.Err(err))
		}
	}
}
