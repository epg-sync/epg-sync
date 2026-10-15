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

type ProgramResponse struct {
	BusinessCode string `json:"businessCode"`
	ResultSet    []struct {
		Date      string `json:"date"`
		Schedules []struct {
			ShowName      string `json:"showName"`
			StartDatetime string `json:"startDatetime"`
			EndDatetime   string `json:"endDatetime"`
		} `json:"schedules"`
	} `json:"resultSet"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "13",
			Name: "海南卫视",
		},
		{
			ID:   "5",
			Name: "三沙卫视",
		},
		{
			ID:   "1",
			Name: "海南自贸",
		},
		{
			ID:   "3",
			Name: "海南新闻",
		},
		{
			ID:   "4",
			Name: "海南公共",
		},
		{
			ID:   "6",
			Name: "海南文旅",
		},
		{
			ID:   "7",
			Name: "海南少儿",
		},
	}
)

type HainanProvider struct {
	*provider.BaseProvider
}

func init() {
	provider.Register(provider.Definition{
		ID:         "hainan",
		BaseURL:    "https://www.hnntv.cn",
		Priority:   2,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &HainanProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *HainanProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "13", "海南卫视", time.Now())

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

func (p *HainanProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, []*model.ChannelMappingInfo{
		{
			ProviderChannelID: providerChannelID,
			CanonicalID:       channelID,
		},
	}, date)
}

func (p *HainanProvider) FetchEPGMultiDay(ctx context.Context, providerChannelID, channelID string, start, end time.Time) (map[string][]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}

	res, err := p.GetWithHeaders(ctx, "/api/schedule/byDay", map[string]string{
		"channelId": providerChannelID,
	}, headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID)

	if err != nil {
		return nil, err
	}

	return programData, nil
}

func (p *HainanProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *HainanProvider) ParseEPGResponse(data []byte, providerChannelID, channelID string) (map[string][]*model.Program, error) {

	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	if resp.BusinessCode != "00000" {
		return nil, errors.ProviderAPIError(p.GetID(), resp.BusinessCode, "API returned business error")
	}

	if len(resp.ResultSet) == 0 {
		return map[string][]*model.Program{}, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	result := make(map[string][]*model.Program)

	for _, programData := range resp.ResultSet {
		schedules := programData.Schedules
		date := programData.Date
		var programs []*model.Program
		for _, schedule := range schedules {
			startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRange(schedule.StartDatetime, schedule.EndDatetime, date, provider.TimeLayoutYYYYMMDDHHMMSS, location)
			if err != nil {
				logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
				continue
			}
			programs = append(programs, &model.Program{
				ChannelID:        channelID,
				Title:            schedule.ShowName,
				StartTime:        startTime,
				EndTime:          endTime,
				OriginalTimezone: provider.UTC8Location,
				ProviderID:       p.GetID(),
			})
		}
		result[date] = programs
	}

	return result, nil
}
