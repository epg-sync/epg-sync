package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/pkg/logger"
	"gorm.io/gorm"
)

func TestGormLoggerLevels(t *testing.T) {
	hub := logger.NewHub(10)
	l, err := logger.NewLogger(&logger.Config{Level: "info", Output: "stdout"}, hub.Core(logger.InfoLevel))
	if err != nil {
		t.Fatal(err)
	}
	gl := &gormLogger{log: l.Named("gorm"), level: newGormLogger(false).(*gormLogger).level}
	sql := func() (string, int64) { return "SELECT 1", 0 }
	ctx := context.Background()

	gl.Trace(ctx, time.Now(), sql, gorm.ErrRecordNotFound)         // 业务分支，不记录
	gl.Trace(ctx, time.Now(), sql, context.Canceled)               // 请求方断开，不记录
	gl.Trace(ctx, time.Now(), sql, nil)                            // 正常查询，不记录
	gl.Trace(ctx, time.Now(), sql, errors.New("connection reset")) // 记录为 error
	gl.Trace(ctx, time.Now().Add(-time.Second), sql, nil)          // 慢查询，记录为 warn

	var got []logger.Record
	for len(hub.Records()) > 0 {
		got = append(got, <-hub.Records())
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(got), got)
	}
	if got[0].Level != logger.ErrorLevel || got[0].Logger != "gorm" || got[0].Fields["sql"] != "SELECT 1" {
		t.Errorf("unexpected error record %+v", got[0])
	}
	if got[1].Level != logger.WarnLevel || got[1].Message != "Slow database query" {
		t.Errorf("unexpected slow query record %+v", got[1])
	}
}
