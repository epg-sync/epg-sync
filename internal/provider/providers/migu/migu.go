package migu

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
	Code    int    `json:"code"`
	Message string `json:"message"`
	Body    struct {
		Program []struct {
			Content []struct {
				ContName  string `json:"contName"`
				StartTime int64  `json:"startTime"`
				EndTime   int64  `json:"endTime"`
			} `json:"content"`
		} `json:"program"`
	} `json:"body"`
}

func init() {
	provider.Register(provider.Definition{
		ID:         "migu_tv",
		BaseURL:    "https://program-sc.miguvideo.com",
		Priority:   4,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

type MiguProvider struct {
	*provider.BaseProvider
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &MiguProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *MiguProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "608807420", "CCTV1", time.Now())

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

func (p *MiguProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}

	path := fmt.Sprintf("/live/v2/tv-programs-data/%s/%s", providerChannelID, date.Format("20060102"))

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

func (p *MiguProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *MiguProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, formatDate string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}
	if resp.Code != 200 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.Code), resp.Message)
	}
	var programs []*model.Program

	if len(resp.Body.Program) == 0 {
		return programs, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp.Body.Program[0].Content {

		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRangeFromTimestamp(programData.StartTime, programData.EndTime, formatDate, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, formatDate).Error())
			continue
		}

		programs = append(programs, &model.Program{
			ChannelID:        channelID,
			Title:            programData.ContName,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return programs, nil
}
