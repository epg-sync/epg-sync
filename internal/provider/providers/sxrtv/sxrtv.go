package sxrtv

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type ProgramResponse []struct {
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "SXTV1",
			Name: "山西卫视",
		},
		{
			ID:   "SXTV2",
			Name: "黄河电视台",
		},
		{
			ID:   "SXTV3",
			Name: "山西经济与科技",
		},
		{
			ID:   "SXTV4",
			Name: "山西影视",
		},
		{
			ID:   "SXTV5",
			Name: "山西社会与法制",
		},
		{
			ID:   "SXTV6",
			Name: "山西文体生活",
		},
	}
)

type SXRTVProvider struct {
	*provider.BaseProvider
}

func init() {
	provider.Register(provider.Definition{
		ID:         "sxrtv",
		BaseURL:    "https://apphhplushttps.sxrtv.com",
		Priority:   3,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &SXRTVProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *SXRTVProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "SXTV1", "山西卫视", time.Now())

	if err != nil {
		return &model.ProviderHealth{
			Healthy: false,
			Message: fmt.Sprintf("FetchEPG failed: %v", err),
		}
	}

	return &model.ProviderHealth{
		Healthy: true,
		Message: "OK",
	}
}

func (p *SXRTVProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, []*model.ChannelMappingInfo{
		{
			ProviderChannelID: providerChannelID,
			CanonicalID:       channelID,
		},
	}, date)
}

func (p *SXRTVProvider) FetchEPGMultiDay(ctx context.Context, providerChannelID, channelID string, start, end time.Time) (map[string][]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}

	path := fmt.Sprintf("/epg/%s.json", providerChannelID)

	res, err := p.GetWithHeaders(ctx, path, nil, headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID)
	if err != nil {
		return nil, err
	}
	return programData, nil
}

func (p *SXRTVProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *SXRTVProvider) ParseEPGResponse(data []byte, providerChannelID, channelID string) (map[string][]*model.Program, error) {
	dataStr := string(data)
	dataStr = dataStr[23 : len(dataStr)-4]

	var resp ProgramResponse
	err := json.Unmarshal([]byte(dataStr), &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	var result = make(map[string][]*model.Program)

	if len(resp) == 0 {
		return result, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp {
		dateStr := programData.StartTime[:10]
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRange(programData.StartTime, programData.EndTime, dateStr, provider.TimeLayoutYYYYMMDDHHMMSS, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, dateStr).Error())
			continue
		}
		result[dateStr] = append(result[dateStr], &model.Program{
			ChannelID:        channelID,
			Title:            programData.Name,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return result, nil
}
