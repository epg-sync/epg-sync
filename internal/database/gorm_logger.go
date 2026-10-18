package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/pkg/logger"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	slowQueryThreshold = 500 * time.Millisecond
	// 批量写入节目时 SQL 很长，日志里只保留开头
	maxLoggedSQL = 2000
)

// gormLogger 把 gorm 的日志转到项目统一的 logger，这样数据库错误也能在管理后台看到。
// gorm 自带的 logger 直接写 stdout，而且会把「查不到记录」当作错误打印。
type gormLogger struct {
	log   logger.Logger
	level gormlogger.LogLevel
}

func newGormLogger(debug bool) gormlogger.Interface {
	level := gormlogger.Warn
	if debug {
		level = gormlogger.Info
	}
	return &gormLogger{log: logger.Named("gorm"), level: level}
}

func (l *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

func (l *gormLogger) Info(_ context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Info {
		l.log.Info(fmt.Sprintf(msg, args...))
	}
}

func (l *gormLogger) Warn(_ context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Warn {
		l.log.Warn(fmt.Sprintf(msg, args...))
	}
}

func (l *gormLogger) Error(_ context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Error {
		l.log.Error(fmt.Sprintf(msg, args...))
	}
}

func (l *gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)

	switch {
	// 查不到记录是正常的业务分支，由调用方处理
	case errors.Is(err, gorm.ErrRecordNotFound):
	// 请求方断开连接（如浏览器刷新、客户端超时）导致查询中止，不是数据库的问题
	case errors.Is(err, context.Canceled):
		if l.level >= gormlogger.Info {
			sql, rows := fc()
			l.log.Debug("Database query canceled", sqlFields(sql, rows, elapsed)...)
		}
	case err != nil && l.level >= gormlogger.Error:
		sql, rows := fc()
		l.log.Error("Database query failed", append(sqlFields(sql, rows, elapsed), logger.Err(err))...)
	case elapsed > slowQueryThreshold && l.level >= gormlogger.Warn:
		sql, rows := fc()
		l.log.Warn("Slow database query", sqlFields(sql, rows, elapsed)...)
	case l.level >= gormlogger.Info:
		sql, rows := fc()
		l.log.Debug("Database query", sqlFields(sql, rows, elapsed)...)
	}
}

func sqlFields(sql string, rows int64, elapsed time.Duration) []logger.Field {
	if len(sql) > maxLoggedSQL {
		sql = strings.ToValidUTF8(sql[:maxLoggedSQL], "") + "…"
	}
	return []logger.Field{
		logger.String("sql", sql),
		logger.Int64("rows", rows),
		logger.Duration("elapsed", elapsed),
	}
}
