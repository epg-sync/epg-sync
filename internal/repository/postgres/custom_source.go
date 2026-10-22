package postgres

import (
	"context"
	"strconv"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
	"gorm.io/gorm"
)

type customSourceRepo struct {
	*BaseRepository
}

func NewCustomSourceRepository(db *gorm.DB) repository.CustomSourceRepository {
	return &customSourceRepo{BaseRepository: NewBaseRepository(db)}
}

func (r *customSourceRepo) Create(ctx context.Context, source *model.CustomSource) error {
	source.CreatedAt = time.Now()
	source.UpdatedAt = source.CreatedAt

	if err := r.db.WithContext(ctx).Create(source).Error; err != nil {
		logger.Error("Failed to create custom source", logger.Err(err), logger.String("name", source.Name))
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to create custom source")
	}
	return nil
}

func (r *customSourceRepo) List(ctx context.Context) ([]*model.CustomSource, error) {
	var sources []*model.CustomSource
	err := r.db.WithContext(ctx).Order("priority ASC, id ASC").Find(&sources).Error
	if err != nil {
		logger.Error("Failed to list custom sources", logger.Err(err))
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list custom sources")
	}
	return sources, nil
}

func (r *customSourceRepo) GetByID(ctx context.Context, id int64) (*model.CustomSource, error) {
	var source model.CustomSource
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&source).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NotFound("custom source", strconv.FormatInt(id, 10))
		}
		logger.Error("Failed to get custom source", logger.Err(err), logger.Int64("id", id))
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to get custom source")
	}
	return &source, nil
}

// Update 只更新用户可编辑的字段；同步状态由 SetRunning / SetSyncResult 维护
func (r *customSourceRepo) Update(ctx context.Context, source *model.CustomSource) error {
	source.UpdatedAt = time.Now()

	result := r.db.WithContext(ctx).
		Model(&model.CustomSource{}).
		Where("id = ?", source.ID).
		Updates(map[string]any{
			"name":       source.Name,
			"url":        source.URL,
			"mode":       source.Mode,
			"enabled":    source.Enabled,
			"priority":   source.Priority,
			"updated_at": source.UpdatedAt,
		})
	if result.Error != nil {
		logger.Error("Failed to update custom source", logger.Err(result.Error), logger.Int64("id", source.ID))
		return errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to update custom source")
	}
	if result.RowsAffected == 0 {
		return errors.NotFound("custom source", strconv.FormatInt(source.ID, 10))
	}
	return nil
}

func (r *customSourceRepo) Delete(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.CustomSource{})
	if result.Error != nil {
		logger.Error("Failed to delete custom source", logger.Err(result.Error), logger.Int64("id", id))
		return errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to delete custom source")
	}
	if result.RowsAffected == 0 {
		return errors.NotFound("custom source", strconv.FormatInt(id, 10))
	}
	return nil
}

func (r *customSourceRepo) SetRunning(ctx context.Context, id int64) error {
	err := r.db.WithContext(ctx).
		Model(&model.CustomSource{}).
		Where("id = ?", id).
		Updates(map[string]any{"last_status": model.CustomSourceStatusRunning, "last_error": "", "last_message": ""}).Error
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to mark custom source running")
	}
	return nil
}

func (r *customSourceRepo) SetSyncResult(ctx context.Context, id int64, status, lastError, message string, programCount int) error {
	err := r.db.WithContext(ctx).
		Model(&model.CustomSource{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_sync_at":  time.Now(),
			"last_status":   status,
			"last_error":    lastError,
			"last_message":  message,
			"program_count": programCount,
		}).Error
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to save custom source sync result")
	}
	return nil
}

func (r *customSourceRepo) ClearSyncResult(ctx context.Context, id int64) error {
	err := r.db.WithContext(ctx).
		Model(&model.CustomSource{}).
		Where("id = ?", id).
		Updates(map[string]any{"last_status": "", "last_error": "", "last_message": "", "program_count": 0}).Error
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to clear custom source sync result")
	}
	return nil
}

// ResetRunning 把残留的 running 状态清掉。进程中途退出会让状态卡在 running，启动时调用
func (r *customSourceRepo) ResetRunning(ctx context.Context) error {
	err := r.db.WithContext(ctx).
		Model(&model.CustomSource{}).
		Where("last_status = ?", model.CustomSourceStatusRunning).
		Updates(map[string]any{
			"last_status": model.CustomSourceStatusFailed,
			"last_error":  "同步被中断（服务重启）",
		}).Error
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to reset running custom sources")
	}
	return nil
}
