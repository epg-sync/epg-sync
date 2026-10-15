package xiamen

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
	Theme     string `json:"theme"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "84",
			Name: "厦门卫视",
		},
		{
			ID:   "16",
			Name: "厦视一套",
		},
		{
			ID:   "17",
			Name: "厦视二套",
		},
	}
)

type XiamenProvider struct {
	*provider.BaseProvider
}

func init() {
	provider.Register(provider.Definition{
		ID:         "xiamen",
		BaseURL:    "https://mapi1.kxm.xmtv.cn",
		Priority:   2,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &XiamenProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *XiamenProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "84", "厦门卫视", time.Now())

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

func (p *XiamenProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}

	path := fmt.Sprintf("/api/v1/tvshow_detail.php?channel_id=%s", providerChannelID)

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

func (p *XiamenProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *XiamenProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
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
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRangeFromTimestamp(programData.StartTime, programData.EndTime, date, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		result = append(result, &model.Program{
			ChannelID:        channelID,
			Title:            programData.Theme,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return result, nil
}
