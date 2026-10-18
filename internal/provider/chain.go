// internal/provider/chain.go
package provider

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type Chain struct {
	providers []Provider
}

func NewChain(cache cache.Cache, providers ...Provider) *Chain {
	chain := &Chain{
		providers: make([]Provider, 0, len(providers)),
	}

	for _, p := range providers {
		if p.IsEnabled() {
			p.SetCache(cache)
			chain.providers = append(chain.providers, p)
		}
	}

	chain.sortByPriority()
	return chain
}

func (c *Chain) sortByPriority() {
	sort.Slice(c.providers, func(i, j int) bool {
		return c.providers[i].GetPriority() < c.providers[j].GetPriority()
	})
}

func (c *Chain) GetProviders() []Provider {
	return c.providers
}

func (c *Chain) FetchEPG(ctx context.Context, channelMappingInfo *model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	providers := c.providers
	if len(providers) == 0 {
		return nil, errors.New(errors.ErrCodeProviderNotFound, "no enabled providers")
	}

	var lastErr error
	channelID, providerChannelID, providerID := channelMappingInfo.CanonicalID, channelMappingInfo.ProviderChannelID, channelMappingInfo.ProviderID
	for _, provider := range providers {
		if !provider.SupportChannel(providerID, providerChannelID) {
			logger.Warn(errors.ProviderNotSupportChannel(provider.GetID(), providerChannelID, channelID).Error())
			continue
		}

		if err := provider.Validate(); err != nil {
			logger.Warn(errors.ProviderInvalidConfig(provider.GetID(), err.Error()).Error())
			lastErr = err
			continue
		}

		logger.Debug("Fetching EPG from provider",
			logger.String("provider_id", provider.GetID()),
			logger.String("channel_id", channelID),
			logger.Time("date", date),
		)

		data, err := provider.FetchEPG(ctx, providerChannelID, channelID, date)
		if err != nil {
			logger.Warn("Provider fetch failed",
				logger.String("provider_id", provider.GetID()),
				logger.Err(err),
			)
			lastErr = err
			continue
		}

		logger.Info("Successfully fetched EPG from provider",
			logger.String("provider_id", provider.GetID()),
			logger.Int("program_count", len(data)),
		)
		return data, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
	}

	return nil, errors.EPGNotFound(channelID, date.Format("2006-01-02"))
}

func (c *Chain) FetchEPGParallel(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	providers := c.providers

	if len(providers) == 0 {
		return nil, errors.New(errors.ErrCodeProviderNotFound, "no enabled providers")
	}

	for _, provider := range providers {
		cmInfos := make([]*model.ChannelMappingInfo, 0)
		for _, cmInfo := range channelMappingInfo {
			if provider.GetID() == cmInfo.ProviderID {
				cmInfos = append(cmInfos, cmInfo)
			}
		}

		// 每批只含部分 provider 的频道，其余 provider 跳过是正常情况
		if len(cmInfos) == 0 {
			logger.Debug("No channel mapping info for provider",
				logger.String("provider_id", provider.GetID()),
			)
			continue
		}

		logger.Debug("Fetching EPG batch from provider",
			logger.String("provider_id", provider.GetID()),
			logger.Int("channel_count", len(cmInfos)),
			logger.Time("date", date),
		)

		data, err := provider.FetchEPGBatch(ctx, cmInfos, date)
		if err != nil {
			logger.Warn("Provider fetch failed",
				logger.String("provider_id", provider.GetID()),
				logger.Err(err),
			)
			continue
		}
		return data, nil
	}

	return nil, errors.New(errors.ErrCodeEPGNotFound, "EPG data not found from any provider")
}
