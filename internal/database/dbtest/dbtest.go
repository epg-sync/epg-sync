// Package dbtest 为集成测试提供独立的 Postgres schema。
// 需要设置 TEST_DATABASE_URL，未设置时测试会被跳过。
package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/config"
	"github.com/epg-sync/epgsync/internal/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// New 创建一个随机 schema 并执行迁移，测试结束后删除
func New(t *testing.T) *gorm.DB {
	t.Helper()

	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	admin, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	ctx := context.Background()
	db, err := database.Open(ctx, config.DatabaseConfig{URL: u.String()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		if sqlDB, err := admin.DB(); err == nil {
			sqlDB.Close()
		}
	})

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}
