package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/pkg/errors"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type logRepo struct {
	db *gorm.DB
	// 写日志用的会话不经过 gorm logger，否则写入出错时会再产生一条日志，形成循环
	quiet *gorm.DB
}

func NewLogRepository(db *gorm.DB) repository.LogRepository {
	return &logRepo{
		db:    db,
		quiet: db.Session(&gorm.Session{Logger: gormlogger.Discard}),
	}
}

func (r *logRepo) CreateBatch(ctx context.Context, logs []*model.Log) error {
	if len(logs) == 0 {
		return nil
	}
	if err := r.quiet.WithContext(ctx).CreateInBatches(logs, 500).Error; err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to write logs")
	}
	return nil
}

func (r *logRepo) List(ctx context.Context, f model.LogFilter) ([]*model.Log, error) {
	q := r.db.WithContext(ctx).Model(&model.Log{})

	if f.MinLevel != nil {
		q = q.Where("level >= ?", *f.MinLevel)
	}
	if f.Logger != nil {
		q = q.Where("logger = ?", *f.Logger)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		pattern := "%" + escapeLike(kw) + "%"
		q = q.Where("(message ILIKE ? OR fields::text ILIKE ? OR caller ILIKE ?)", pattern, pattern, pattern)
	}
	if !f.Since.IsZero() {
		q = q.Where("time >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		q = q.Where("time < ?", f.Until)
	}

	// 实时刷新取比 AfterID 新的最早一批，正序取再翻转，保证连续不漏；其余情况从新到旧
	if f.AfterID > 0 {
		q = q.Where("id > ?", f.AfterID).Order("id ASC")
	} else {
		if f.BeforeID > 0 {
			q = q.Where("id < ?", f.BeforeID)
		}
		q = q.Order("id DESC")
	}

	var logs []*model.Log
	if err := q.Limit(f.Limit).Find(&logs).Error; err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list logs")
	}

	if f.AfterID > 0 {
		for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
			logs[i], logs[j] = logs[j], logs[i]
		}
	}
	return logs, nil
}

func (r *logRepo) DeleteBefore(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("time < ?", before).Delete(&model.Log{})
	if result.Error != nil {
		return 0, errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to delete old logs")
	}
	return result.RowsAffected, nil
}

// escapeLike 转义 LIKE 的通配符，让关键字按字面匹配
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
