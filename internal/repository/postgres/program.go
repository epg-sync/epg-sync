package postgres

import (
	"context"
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
