package postgres

import (
	"context"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 同一个 provider 频道只保留一条映射，重新匹配时覆盖旧结果
var upsertMapping = clause.OnConflict{
	Columns:   []clause.Column{{Name: "provider_id"}, {Name: "provider_channel_id"}},
	DoUpdates: clause.AssignmentColumns([]string{"canonical_id", "provider_channel_name", "confidence", "updated_at"}),
}

type channelMappingsRepo struct {
	*BaseRepository
}

func NewChannelMappingsRepository(db *gorm.DB) repository.ChannelMappingsRepository {
	return &channelMappingsRepo{BaseRepository: NewBaseRepository(db)}
}

func (r *channelMappingsRepo) Create(ctx context.Context, mapping *model.ChannelMapping) error {
	mapping.CreatedAt = time.Now()
	mapping.UpdatedAt = time.Now()

	if err := r.db.WithContext(ctx).Clauses(upsertMapping).Create(mapping).Error; err != nil {
		logger.Error("Failed to create channel mapping",
			logger.Err(err),
			logger.String("channel_id", mapping.ProviderChannelID),
			logger.String("provider_id", mapping.ProviderID),
		)
		return err
	}
	return nil
}

func (r *channelMappingsRepo) CreateBatch(ctx context.Context, mappings []*model.ChannelMapping) error {
	if len(mappings) == 0 {
		return nil
	}

	if err := r.db.WithContext(ctx).Clauses(upsertMapping).Create(mappings).Error; err != nil {
		logger.Error("Failed to create channel mapping",
			logger.Err(err),
			logger.Int("count", len(mappings)),
		)
		return err
	}

	return nil
}

func (r *channelMappingsRepo) GetByProviderChannelID(ctx context.Context, providerChannelID, providerID string) (*model.ChannelMapping, error) {
	var mapping model.ChannelMapping
	err := r.db.WithContext(ctx).
		Where("provider_channel_id = ? AND provider_id = ?", providerChannelID, providerID).
		First(&mapping).Error
	if err != nil {
		return nil, err
	}
	return &mapping, nil
}

func (r *channelMappingsRepo) GetByProviderID(ctx context.Context, providerID string) ([]*model.ChannelMapping, error) {
	var mappings []*model.ChannelMapping
	err := r.db.WithContext(ctx).
		Where("provider_id = ?", providerID).
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *channelMappingsRepo) GetByCanonicalID(ctx context.Context, canonicalChannelID string) ([]*model.ChannelMapping, error) {
	var mappings []*model.ChannelMapping
	err := r.db.WithContext(ctx).
		Where("canonical_id = ?", canonicalChannelID).
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *channelMappingsRepo) ListByCanonicalID(ctx context.Context, canonicalChannelID string) ([]*model.ChannelMapping, error) {
	var mappings []*model.ChannelMapping
	err := r.db.WithContext(ctx).
		Where("canonical_id = ?", canonicalChannelID).
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *channelMappingsRepo) ListAllChannelMappings(ctx context.Context) ([]*model.ChannelMapping, error) {
	var mappings []*model.ChannelMapping
	err := r.db.WithContext(ctx).
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *channelMappingsRepo) ListByProviderID(ctx context.Context, providerID string) ([]*model.ChannelMapping, error) {
	var mappings []*model.ChannelMapping
	err := r.db.WithContext(ctx).
		Where("provider_id = ?", providerID).
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *channelMappingsRepo) DeleteByProviderID(ctx context.Context, providerID string) error {
	return r.db.WithContext(ctx).
		Where("provider_id = ?", providerID).
		Delete(&model.ChannelMapping{}).Error
}
