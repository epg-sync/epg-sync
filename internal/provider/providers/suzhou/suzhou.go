package suzhou

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

type SuzhouProvider struct {
	*provider.BaseProvider
}

type ProgramResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		ProgramList []struct {
			Name      string `json:"name"`
			ChildList []struct {
				ProgramName string `json:"program_name"`
				StartTime   int64  `json:"start_time"`
				EndTime     int64  `json:"end_time"`
			} `json:"child_list"`
		} `json:"program_list"`
	} `json:"data"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "sz4k",
			Name: "苏州4K",
		},
	}
)

func init() {
	provider.Register(provider.Definition{
		ID:         "suzhou",
		BaseURL:    "https://kszapi1.kan0512.com",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &SuzhouProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *SuzhouProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "sz4k", "苏州4K", time.Now())

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

func (p *SuzhouProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, []*model.ChannelMappingInfo{
		{
			ProviderChannelID: providerChannelID,
			CanonicalID:       channelID,
		},
	}, date)
}

func (p *SuzhouProvider) FetchEPGMultiDay(ctx context.Context, providerChannelID, channelID string, start, end time.Time) (map[string][]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}

	logger.Debug("FetchEPGMultiDay time range", logger.Time("start", start), logger.Time("end", end))

	res, err := p.GetWithHeaders(ctx, "/api/app/channel/live4kprogramlist", nil, headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID)

	if err != nil {
		return nil, err
	}

	return programData, nil
}

func (p *SuzhouProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *SuzhouProvider) ParseEPGResponse(data []byte, providerChannelID, channelID string) (map[string][]*model.Program, error) {

	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	if resp.Code != 0 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.Code), resp.Message)
	}

	var result = make(map[string][]*model.Program)
	if len(resp.Data.ProgramList) == 0 {
		return result, nil
	}
	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}
	for _, programData := range resp.Data.ProgramList {
		for _, child := range programData.ChildList {
			dateStr := time.UnixMilli(child.StartTime * 1e3).In(location).Format("2006-01-02")
			startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRangeFromTimestamp(child.StartTime, child.EndTime, dateStr, location)
			if err != nil {
				logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, dateStr).Error())
				continue
			}
			result[dateStr] = append(result[dateStr], &model.Program{
				ChannelID:        channelID,
				Title:            child.ProgramName,
				StartTime:        startTime,
				EndTime:          endTime,
				OriginalTimezone: provider.UTC8Location,
				ProviderID:       p.GetID(),
			})
		}
	}
	return result, nil
}
