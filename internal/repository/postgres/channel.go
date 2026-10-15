package postgres

import (
	"context"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type channelRepo struct {
	*BaseRepository
}

func NewChannelRepository(db *gorm.DB) repository.ChannelRepository {
	return &channelRepo{BaseRepository: NewBaseRepository(db)}
}

func (r *channelRepo) Create(ctx context.Context, channel *model.Channel) error {
	channel.CreatedAt = time.Now()
	channel.UpdatedAt = time.Now()

	if err := r.db.WithContext(ctx).Create(channel).Error; err != nil {
		logger.Error("Failed to create channel",
			logger.Err(err),
			logger.String("channel_id", channel.ChannelID),
		)
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to create channel")
	}

	return nil
}

func (r *channelRepo) CreateBatch(ctx context.Context, channels []*model.Channel) error {
	if len(channels) == 0 {
		return nil
	}
	// 按 channel_id upsert，导入时已存在的频道（如内置种子频道）会被更新
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "channel_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"display_name", "category", "area", "logo_url", "regexp", "timezone", "updated_at"}),
		}).
		Create(channels).Error
	if err != nil {
		logger.Error("Failed to create channels in batch",
			logger.Err(err),
			logger.Int("count", len(channels)),
		)
		return errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to create channels in batch")
	}

	logger.Info("Batch created channels", logger.Int("count", len(channels)))
	return nil
}

func (r *channelRepo) GetAllChannels(ctx context.Context) ([]*model.Channel, error) {
	var channels []*model.Channel
	err := r.db.WithContext(ctx).Find(&channels).Error
	if err != nil {
		logger.Error("Failed to get all channels", logger.Err(err))
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to get all channels")
	}

	return channels, nil
}

func (r *channelRepo) GetByID(ctx context.Context, channelID string) (*model.Channel, error) {
	var channel model.Channel
	err := r.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&channel).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.ChannelNotFound(channelID)
		}
		logger.Error("Failed to get channel",
			logger.Err(err),
			logger.String("channel_id", channelID),
		)
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to get channel")
	}

	return &channel, nil
}

func (r *channelRepo) GetByChannelName(ctx context.Context, channelName string) (*model.Channel, error) {
	var channel model.Channel
	// 不区分大小写的正则匹配，多个频道都匹配时取 id 最小的
	err := r.db.WithContext(ctx).
		Where("regexp <> '' AND ? ~* regexp", channelName).
		Order("id").
		First(&channel).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.ChannelNotFound(channelName)
		}
		return nil, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to get channel by name")
	}

	return &channel, nil

}

func (r *channelRepo) Update(ctx context.Context, channel *model.Channel) error {
	channel.UpdatedAt = time.Now()

	result := r.db.WithContext(ctx).
		Model(&model.Channel{}).
		Where("id = ?", channel.ID).
		Updates(map[string]any{
			"channel_id":   channel.ChannelID,
			"display_name": channel.DisplayName,
			"logo_url":     channel.LogoURL,
			"category":     channel.Category,
			"area":         channel.Area,
			"regexp":       channel.Regexp,
			"is_active":    channel.IsActive,
			"timezone":     channel.Timezone,
			"updated_at":   channel.UpdatedAt,
		})
	if result.Error != nil {
		logger.Error("Failed to update channel",
			logger.Err(result.Error),
			logger.String("channel_id", channel.ChannelID),
		)
		return errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to update channel")
	}

	if result.RowsAffected == 0 {
		return errors.ChannelNotFound(channel.ChannelID)
	}

	return nil
}

func (r *channelRepo) Delete(ctx context.Context, channelID string) error {
	result := r.db.WithContext(ctx).Where("channel_id = ?", channelID).Delete(&model.Channel{})
	if result.Error != nil {
		logger.Error("Failed to delete channel",
			logger.Err(result.Error),
			logger.String("channel_id", channelID),
		)
		return errors.Wrap(result.Error, errors.ErrCodeDatabaseQuery, "failed to delete channel")
	}

	if result.RowsAffected == 0 {
		return errors.ChannelNotFound(channelID)
	}

	return nil
}

func (r *channelRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Channel{}).Count(&count).Error
	if err != nil {
		logger.Error("Failed to count channels", logger.Err(err))
		return 0, errors.Wrap(err, errors.ErrCodeDatabaseQuery, "failed to count channels")
	}

	return count, nil
}
