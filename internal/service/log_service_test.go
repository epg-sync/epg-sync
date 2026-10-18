package service

import (
	"context"
	"testing"

	"github.com/epg-sync/epgsync/internal/database/dbtest"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository/postgres"
	"github.com/epg-sync/epgsync/pkg/logger"
)

// 启动前缓冲的日志和停止前的日志都应写入数据库
func TestLogServiceFlushesBufferedAndPendingEntries(t *testing.T) {
	db := dbtest.New(t)
	hub := logger.NewHub(100)
	l, err := logger.NewLogger(&logger.Config{Level: "info", Output: "stdout"}, hub.Core(logger.InfoLevel))
	if err != nil {
		t.Fatal(err)
	}

	l.Info("before start")
	svc := NewLogService(postgres.NewLogRepository(db), hub, 30)
	svc.Start()
	l.Named("http").Error("after start", logger.String("path", "/api/diyp"))
	svc.Stop()

	logs, err := svc.List(context.Background(), model.LogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Message != "after start" || logs[1].Message != "before start" {
		t.Fatalf("unexpected logs: %+v", logs)
	}
	if logs[0].Logger != "http" || logs[0].Level != int8(logger.ErrorLevel) || logs[0].Fields["path"] != "/api/diyp" {
		t.Errorf("entry not stored as structured data: %+v", logs[0])
	}
}
