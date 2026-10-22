package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
	"gorm.io/gorm"
)

type programRepo struct {
	*BaseRepository
}

func NewProgramRepository(db *gorm.DB) repository.ProgramRepository {
	return &programRepo{BaseRepository: NewBaseRepository(db)}
}

func (r *programRepo) Create(ctx context.Context, program *model.Program) error {
	program.CreatedAt = time.Now()

	if err := r.db.WithContext(ctx).Create(program).Error; err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to create program")
	}

	return nil
}

func (r *programRepo) CreateBatch(ctx context.Context, programs []*model.Program) error {
	if len(programs) == 0 {
		return nil
	}
	// 分批写入，避免超过 Postgres 单条语句 65535 个参数的上限
	if err := r.db.WithContext(ctx).CreateInBatches(programs, 1000).Error; err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to create programs in batch")
	}

	return nil

}

func (r *programRepo) ListByChannelIDAndDate(ctx context.Context, channelID string, date time.Time, page, pageSize int) ([]*model.Program, int64, error) {
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)

	logger.Debug("Listing programs by channel and date",
		logger.String("channel_id", channelID),
		logger.Time("start_of_day", startOfDay),
		logger.Time("end_of_day", endOfDay),
	)

	startOfDay = startOfDay.In(time.UTC)
	endOfDay = endOfDay.In(time.UTC)

	logger.Debug("Listing programs by channel and date",
		logger.String("channel_id", channelID),
		logger.Time("start_of_day", startOfDay),
		logger.Time("end_of_day", endOfDay),
	)

	var programs []*model.Program
	var total int64

	query := r.db.WithContext(ctx).Model(&model.Program{})
	if channelID != "" {
		query = query.Where("channel_id = ?", channelID)
	}
	query = query.Where("start_time >= ? AND start_time < ?", startOfDay, endOfDay)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to count programs")
	}

	err := query.
		Order("start_time ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&programs).Error
	if err != nil {
		return nil, 0, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list programs")
	}

	return programs, total, nil
}

func (r *programRepo) ListByChannelAndDate(ctx context.Context, channel *model.Channel, date string) ([]*model.Program, error) {
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "invalid date format")
	}
	loc, err := time.LoadLocation(channel.Timezone)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "invalid timezone")
	}
	startOfDay := time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, loc)
	endOfDay := startOfDay.Add(24 * time.Hour)

	startOfDay = startOfDay.In(time.UTC)
	endOfDay = endOfDay.In(time.UTC)

	var programs []*model.Program
	err = r.db.WithContext(ctx).
		Where("channel_id = ? AND start_time >= ? AND start_time < ?", channel.ChannelID, startOfDay, endOfDay).
		Order("start_time ASC").
		Find(&programs).Error
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list programs")
	}

	return programs, nil
}

func (r *programRepo) ListByChannelIDAndTimeRange(ctx context.Context, channelID string, start, end time.Time) ([]*model.Program, error) {
	var programs []*model.Program
	err := r.db.WithContext(ctx).
		Where("channel_id = ? AND start_time >= ? AND start_time < ?", channelID, start, end).
		Order("start_time ASC").
		Find(&programs).Error
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list programs")
	}

	return programs, nil
}

func (r *programRepo) ListAllByDateRange(ctx context.Context, start time.Time, end time.Time) ([]*model.Program, error) {
	startOfDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endOfDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location()).Add(24 * time.Hour)

	startOfDay = startOfDay.In(time.UTC)
	endOfDay = endOfDay.In(time.UTC)

	logger.Debug("Listing programs by date range",
		logger.Time("start_of_day", startOfDay),
		logger.Time("end_of_day", endOfDay),
	)

	var programs []*model.Program
	err := r.db.WithContext(ctx).
		Preload("Channel").
		Where("start_time >= ? AND start_time < ?", startOfDay, endOfDay).
		Order("channel_id ASC, start_time ASC").
		Find(&programs).Error
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list programs by date range")
	}
	return programs, nil
}

func (r *programRepo) GetCurrentProgram(ctx context.Context, channelID string) (*model.Program, error) {
	now := time.Now()

	var program model.Program
	err := r.db.WithContext(ctx).
		Where("channel_id = ? AND start_time <= ? AND end_time > ?", channelID, now, now).
		Order("start_time DESC").
		First(&program).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.EPGNotFound(channelID, now.Format("2006-01-02"))
		}
		logger.Error("Failed to get current program",
			logger.Err(err),
			logger.String("channel_id", channelID),
		)
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to get current program")
	}

	return &program, nil
}

func (r *programRepo) DeleteBefore(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("end_time < ?", before).
		Delete(&model.Program{})
	if result.Error != nil {
		logger.Error("Failed to delete old programs",
			logger.Err(result.Error),
			logger.Time("before", before),
		)
		return 0, errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to delete old programs")
	}

	logger.Info("Deleted old programs",
		logger.Time("before", before),
		logger.Int64("count", result.RowsAffected),
	)

	return result.RowsAffected, nil
}

func (r *programRepo) ReplaceChannelDay(ctx context.Context, channelID string, date time.Time, programs []*model.Program) error {
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)

	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("channel_id = ? AND start_time >= ? AND start_time < ?", channelID, startOfDay, endOfDay).
			Delete(&model.Program{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected
		if len(programs) == 0 {
			return nil
		}
		return tx.CreateInBatches(programs, 1000).Error
	})
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to replace programs")
	}

	logger.Debug("Replaced programs",
		logger.String("channel_id", channelID),
		logger.Time("date", date),
		logger.Int64("deleted", deleted),
		logger.Int("inserted", len(programs)),
	)
	return nil
}

// fillProviderPattern 匹配增量源写入的节目
var fillProviderPattern = model.CustomSourceFillProviderPrefix + "%"

func (r *programRepo) ReplaceChannelDays(ctx context.Context, channelID string, days []model.TimeRange, providerID string, ownFrom time.Time, programs []*model.Program) error {
	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("channel_id = ? AND provider_id = ? AND end_time > ?", channelID, providerID, ownFrom).
			Delete(&model.Program{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected

		if len(days) > 0 {
			conds := make([]string, len(days))
			args := make([]any, 0, 1+2*len(days))
			args = append(args, channelID)
			for i, d := range days {
				conds[i] = "(start_time >= ? AND start_time < ?)"
				args = append(args, d.From, d.To)
			}
			result := tx.Where("channel_id = ? AND ("+strings.Join(conds, " OR ")+")", args...).
				Delete(&model.Program{})
			if result.Error != nil {
				return result.Error
			}
			deleted += result.RowsAffected
		}

		if len(programs) == 0 {
			return nil
		}
		return tx.CreateInBatches(programs, 1000).Error
	})
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to replace programs by day")
	}

	logger.Debug("Replaced programs by day",
		logger.String("channel_id", channelID),
		logger.String("provider_id", providerID),
		logger.Int("days", len(days)),
		logger.Int64("deleted", deleted),
		logger.Int("inserted", len(programs)),
	)
	return nil
}

func (r *programRepo) ListProviderDays(ctx context.Context, channelID, timezone string, from, to time.Time) ([]*model.ProviderDay, error) {
	var days []*model.ProviderDay
	err := r.db.WithContext(ctx).
		Model(&model.Program{}).
		Select("DISTINCT COALESCE(provider_id, '') AS provider_id, to_char(start_time AT TIME ZONE ?, 'YYYY-MM-DD') AS day", timezone).
		Where("channel_id = ? AND start_time >= ? AND start_time < ?", channelID, from, to).
		Scan(&days).Error
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list provider days")
	}
	return days, nil
}

func (r *programRepo) CreateBatchReplacingFill(ctx context.Context, programs []*model.Program) error {
	if len(programs) == 0 {
		return nil
	}

	// 各频道新节目覆盖的时间范围。按实际时间删除而不是按「天」，不受服务器时区影响；
	// 同一天里范围之外剩下的增量节目，由自定义源下一次同步时清理
	ranges := make(map[string]*model.TimeRange)
	for _, p := range programs {
		rg, ok := ranges[p.ChannelID]
		if !ok {
			ranges[p.ChannelID] = &model.TimeRange{From: p.StartTime, To: p.EndTime}
			continue
		}
		if p.StartTime.Before(rg.From) {
			rg.From = p.StartTime
		}
		if p.EndTime.After(rg.To) {
			rg.To = p.EndTime
		}
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for channelID, rg := range ranges {
			err := tx.Where("channel_id = ? AND provider_id LIKE ? AND start_time < ? AND end_time > ?",
				channelID, fillProviderPattern, rg.To, rg.From).
				Delete(&model.Program{}).Error
			if err != nil {
				return err
			}
		}
		return tx.CreateInBatches(programs, 1000).Error
	})
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to create programs in batch")
	}
	return nil
}

func (r *programRepo) DeleteByProviderOutsideChannels(ctx context.Context, providerID string, keep []string, after time.Time) (int64, error) {
	query := r.db.WithContext(ctx).Where("provider_id = ? AND end_time > ?", providerID, after)
	if len(keep) > 0 {
		query = query.Where("channel_id NOT IN ?", keep)
	}
	result := query.Delete(&model.Program{})
	if result.Error != nil {
		logger.Error("Failed to delete stale programs by provider",
			logger.Err(result.Error),
			logger.String("provider_id", providerID),
		)
		return 0, errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to delete stale programs by provider")
	}
	return result.RowsAffected, nil
}

func (r *programRepo) ListChannelIDsByProvider(ctx context.Context, providerID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&model.Program{}).
		Distinct("channel_id").
		Where("provider_id = ?", providerID).
		Order("channel_id").
		Pluck("channel_id", &ids).Error
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to list channels by provider")
	}
	return ids, nil
}

func (r *programRepo) DeleteByProviderID(ctx context.Context, providerID string) (int64, error) {
	result := r.db.WithContext(ctx).Where("provider_id = ?", providerID).Delete(&model.Program{})
	if result.Error != nil {
		logger.Error("Failed to delete programs by provider",
			logger.Err(result.Error),
			logger.String("provider_id", providerID),
		)
		return 0, errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to delete programs by provider")
	}
	return result.RowsAffected, nil
}

func (r *programRepo) Exists(ctx context.Context, channelID string, date time.Time) (bool, error) {
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)
	startOfDay = startOfDay.In(time.UTC)
	endOfDay = endOfDay.In(time.UTC)

	logger.Debug("Checking program existence",
		logger.String("channel_id", channelID),
		logger.Time("start_of_day", startOfDay),
		logger.Time("end_of_day", endOfDay),
	)
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Program{}).
		Where("channel_id = ? AND start_time >= ? AND start_time < ?", channelID, startOfDay, endOfDay).
		Where("COALESCE(provider_id, '') NOT LIKE ?", fillProviderPattern).
		Count(&count).Error
	if err != nil {
		logger.Error("Failed to check program existence",
			logger.Err(err),
			logger.String("channel_id", channelID),
		)
		return false, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to check existence")
	}

	return count > 0, nil
}
