package service

import (
	"context"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/internal/repository"
)

type ChannelService struct {
	channelRepo        repository.ChannelRepository
	channelMappingRepo repository.ChannelMappingsRepository
	cache              cache.Cache
	chain              *provider.Chain
}

func NewChannelService(
	channelRepo repository.ChannelRepository,
	channelMappingRepo repository.ChannelMappingsRepository,
	cache cache.Cache,
	chain *provider.Chain,
) *ChannelService {
	return &ChannelService{
		channelRepo:        channelRepo,
		channelMappingRepo: channelMappingRepo,
		cache:              cache,
		chain:              chain,
	}
}

func (s *ChannelService) GetChannel(ctx context.Context, channelID string) (*model.Channel, error) {
	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}

	return channel, nil
}

func (s *ChannelService) CreateChannel(ctx context.Context, channel *model.Channel) (*model.Channel, error) {
	if err := s.channelRepo.Create(ctx, channel); err != nil {
		return nil, err
	}

	return channel, nil
}

func (s *ChannelService) BatchCreateChannels(ctx context.Context, channels []*model.Channel) ([]*model.Channel, error) {
	if err := s.channelRepo.CreateBatch(ctx, channels); err != nil {
		return nil, err
	}
	return channels, nil
}

func (s *ChannelService) GetChannelCount(ctx context.Context) (int64, error) {
	return s.channelRepo.Count(ctx)
}

func (s *ChannelService) ListAllChannels(ctx context.Context) ([]*model.Channel, error) {
	return s.channelRepo.GetAllChannels(ctx)
}

func (s *ChannelService) UpdateChannel(ctx context.Context, channel *model.Channel) error {
	if err := s.channelRepo.Update(ctx, channel); err != nil {
		return err
	}

	return nil
}

func (s *ChannelService) DeleteChannel(ctx context.Context, channelID string) error {
	if err := s.channelRepo.Delete(ctx, channelID); err != nil {
		return err
	}

	return nil
}

func (s *ChannelService) ListAllChannelMappings(ctx context.Context) ([]*model.ChannelMapping, error) {
	var allMappings []*model.ChannelMapping

	allMappings, err := s.channelMappingRepo.ListAllChannelMappings(ctx)
	if err != nil {
		return nil, err
	}

	return allMappings, nil
}

func (s *ChannelService) GetChannelMappings(ctx context.Context, channelID string) ([]*model.ChannelMapping, error) {
	return s.channelMappingRepo.GetByCanonicalID(ctx, channelID)
}
