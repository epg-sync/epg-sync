package shanxi

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
	Name  string `json:"name"`
	Start string `json:"start"`
	End   string `json:"end"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "star",
			Name: "陕西卫视",
		},
		{
			ID:   "1",
			Name: "陕西新闻资讯",
		},
		{
			ID:   "2",
			Name: "陕西都市青春",
		},
		{
			ID:   "3",
			Name: "陕西银龄频道",
		},
		{
			ID:   "5",
			Name: "陕西秦腔频道",
		},
		{
			ID:   "7",
			Name: "陕西体育休闲",
		},
		{
			ID:   "nl",
			Name: "农林卫视",
		},
		{
			ID:   "11",
			Name: "陕西移动电视",
		},
	}
)

type ShanxiProvider struct {
	*provider.BaseProvider
}

func init() {
	provider.Register(provider.Definition{
		ID:         "shanxi",
		BaseURL:    "http://qidian.sxtvs.com",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &ShanxiProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *ShanxiProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "star", "陕西卫视", time.Now())

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

func (p *ShanxiProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}

	path := fmt.Sprintf("/api/v3/program/tv?channel=%s", providerChannelID)

	res, err := p.GetWithHeaders(ctx, path, nil, headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID, date.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	return programData, nil
}

func (p *ShanxiProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *ShanxiProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	dataStr := string(data)
	dataStr = dataStr[19 : len(dataStr)-1]

	var resp ProgramResponse
	err := json.Unmarshal([]byte(dataStr), &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	var result = make([]*model.Program, 0)

	if len(resp) == 0 {
		return result, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp {
		start := fmt.Sprintf("%s %s", date, programData.Start)
		end := fmt.Sprintf("%s %s", date, programData.End)
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRange(start, end, date, "2006-01-02 15:04", location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		result = append(result, &model.Program{
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
