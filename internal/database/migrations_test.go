package database

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/config"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newEmptySchema 创建一个空的独立 schema，不执行迁移。
// 不能用 dbtest：dbtest 依赖本包，会形成循环引用。
func newEmptySchema(t *testing.T) *gorm.DB {
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

	u, _ := url.Parse(baseURL)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	db, err := Open(context.Background(), config.DatabaseConfig{URL: u.String()})
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
	return db
}

func newProvider(t *testing.T, db *gorm.DB) *goose.Provider {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// 已经部署了 1.0.1（迁移到第 3 版）的库：00004 应把 CGTN记录 改名为 CGTN纪录，
// 并通过 ON UPDATE CASCADE 带上已有的映射和节目
func TestFixCGTNDocumentaryOnExistingDatabase(t *testing.T) {
	db := newEmptySchema(t)
	ctx := context.Background()
	p := newProvider(t, db)

	if _, err := p.UpTo(ctx, 3); err != nil {
		t.Fatalf("migrate to 3: %v", err)
	}
	mustExec(t, db, `INSERT INTO channel_mappings (canonical_id, provider_id, provider_channel_id, confidence)
		VALUES ('CGTN记录', 'ysp', '600084781', 0.9)`)
	mustExec(t, db, `INSERT INTO programs (channel_id, title, start_time, end_time)
		VALUES ('CGTN记录', 'p', now(), now() + interval '1 hour')`)

	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	assertCount(t, db, `SELECT count(*) FROM channels WHERE channel_id = 'CGTN记录'`, 0)
	assertCount(t, db, `SELECT count(*) FROM channels WHERE channel_id = 'CGTN纪录' AND display_name = 'CGTN纪录'`, 1)
	assertCount(t, db, `SELECT count(*) FROM channel_mappings WHERE canonical_id = 'CGTN纪录'`, 1)
	assertCount(t, db, `SELECT count(*) FROM programs WHERE channel_id = 'CGTN纪录'`, 1)
	assertCount(t, db, `SELECT count(*) FROM channels`, 190)

	// 用户名称里的「纪录」、常见错别字「记录」、英文名都能查到
	for _, name := range []string{"CGTN纪录", "CGTN记录", "cgtn documentary"} {
		var id string
		err := db.Raw(`SELECT channel_id FROM channels WHERE regexp <> '' AND ? ~* regexp ORDER BY id LIMIT 1`, name).Scan(&id).Error
		if err != nil || id != "CGTN纪录" {
			t.Errorf("%q resolved to %q (err %v), want CGTN纪录", name, id, err)
		}
	}
}

// 回滚 00004 应恢复原状，再次执行不报错
func TestFixCGTNDocumentaryDownUp(t *testing.T) {
	db := newEmptySchema(t)
	ctx := context.Background()
	p := newProvider(t, db)

	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 3); err != nil {
		t.Fatalf("down to 3: %v", err)
	}
	assertCount(t, db, `SELECT count(*) FROM channels WHERE channel_id = 'CGTN记录'`, 1)
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	assertCount(t, db, `SELECT count(*) FROM channels WHERE channel_id = 'CGTN纪录'`, 1)
}

func mustExec(t *testing.T, db *gorm.DB, sql string) {
	t.Helper()
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func assertCount(t *testing.T, db *gorm.DB, sql string, want int64) {
	t.Helper()
	var got int64
	if err := db.Raw(sql).Scan(&got).Error; err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	if got != want {
		t.Errorf("%s = %d, want %d", sql, got, want)
	}
}
