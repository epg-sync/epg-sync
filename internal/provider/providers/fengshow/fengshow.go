package ifeng

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type ProgramResponse []struct {
	Title     string `json:"title"`
	EventTime string `json:"event_time"`
}

var (
	userAgent   = "FengWatch/5.5.1 (iPhone; iOS 17.0; Scale/3.00)"
	channelList = []*model.ProviderChannel{
		{
			ID:   "f7f48462-9b13-485b-8101-7b54716411ec",
			Name: "凤凰卫视中文台",
		},
		{
			ID:   "7c96b084-60e1-40a9-89c5-682b994fb680",
			Name: "凤凰卫视资讯台",
		},
		{
			ID:   "15e02d92-1698-416c-af2f-3e9a872b4d78",
			Name: "凤凰卫视香港台",
		},
	}
)

type IfengProvider struct {
	*provider.BaseProvider
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &IfengProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func init() {
	provider.Register(provider.Definition{
		ID:         "fengshow",
		BaseURL:    "https://m.fengshows.com",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func (p *IfengProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "7c96b084-60e1-40a9-89c5-682b994fb680", "凤凰中文", time.Now())

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

func (p *IfengProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: userAgent,
	}
	path := fmt.Sprintf("/api/v3/live/%s/resources", providerChannelID)
	res, err := p.GetWithHeaders(ctx, path, map[string]string{
		"date":      date.Format("20060102"),
		"dir":       "asc",
		"page":      "1",
		"page_size": "80",
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

func (p *IfengProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *IfengProvider) ParseEPGResponse(data []byte, providerChannelID, channelID string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	var programs []*model.Program

	if len(resp) == 0 {
		return programs, nil
	}
	var nextEndTime time.Time
	for i := len(resp) - 1; i >= 0; i-- {
		programData := resp[i]
		startTime, err := time.ParseInLocation("2006-01-02T15:04:05.000Z", programData.EventTime, time.UTC)
		if err != nil {
			logger.Error("Failed to parse event time", logger.Err(err))
			continue
		}

		if nextEndTime.IsZero() {
			nextEndTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 15, 59, 59, 0, time.UTC).Add(24 * time.Hour)
		}

		programs = append(programs, &model.Program{
			ChannelID:        channelID,
			Title:            programData.Title,
			StartTime:        startTime,
			EndTime:          nextEndTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
		nextEndTime = startTime
	}
	slices.Reverse(programs)
	return programs, nil
}
