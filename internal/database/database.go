package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/epg-sync/epgsync/internal/config"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const (
	defaultMaxOpenConns = 10
	connectTimeout      = 30 * time.Second
)

// Open 连接 Postgres，数据库还没就绪时（如 compose 刚启动）会重试一段时间
func Open(ctx context.Context, cfg config.DatabaseConfig) (*gorm.DB, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	var (
		db      *gorm.DB
		err     error
		backoff = 500 * time.Millisecond
	)
	for {
		db, err = gorm.Open(postgres.Open(cfg.URL), &gorm.Config{})
		if err == nil {
			break
		}

		logger.Warn("Database not ready, retrying", logger.Err(err), logger.Duration("backoff", backoff))
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connect to database: %w", err)
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Second)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = defaultMaxOpenConns
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxOpen)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if cfg.Debug {
		db = db.Debug()
	}

	return db, nil
}

// Migrate 执行内置的建表和初始数据迁移，已执行过的版本会跳过
func Migrate(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	for _, r := range results {
		logger.Info("Applied migration",
			logger.String("source", r.Source.Path),
			logger.Duration("duration", r.Duration),
		)
	}

	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	logger.Info("Database schema up to date", logger.Int64("version", version))

	return nil
}
