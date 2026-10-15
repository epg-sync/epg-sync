package btzx

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
	ErrorCode     int    `json:"error_code"`
	Succeed       int    `json:"succeed"`
	ErrorDesc     string `json:"error_desc"`
	VideoLiveList []struct {
		Title     string `json:"title"`
		StartDate string `json:"startdate"`
		EndDate   string `json:"enddate"`
	} `json:"videoLiveList"`
}

func init() {
	provider.Register(provider.Definition{
		ID:         "btzx",
		BaseURL:    "https://api.btzx.com.cn",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

type BTZXProvider struct {
	*provider.BaseProvider
}

var channelList = []*model.ProviderChannel{{
	ID:   "TvCh1540979167111228",
	Name: "兵团卫视",
}}

func New(config *model.ProviderConfig) (provider.Provider, error) {

	return &BTZXProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *BTZXProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "TvCh1540979167111228", "兵团卫视", time.Now())

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

func (p *BTZXProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}
	formatDate := date.Format("2006-01-02")
	parameter := fmt.Sprintf("{'id':'%s','day':'%s'}", providerChannelID, formatDate)
	res, err := p.GetWithHeaders(ctx, "/mobileinf/rest/cctv/videolivelist/dayWeb", map[string]string{
		"json": parameter,
	}, headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID, formatDate)

	if err != nil {
		return nil, err
	}

	return programData, nil
}

func (p *BTZXProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *BTZXProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	if resp.Succeed != 1 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.ErrorCode), resp.ErrorDesc)
	}

	var programs []*model.Program

	if len(resp.VideoLiveList) == 0 {
		return programs, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp.VideoLiveList {
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRange(programData.StartDate, programData.EndDate, date, provider.TimeLayoutYYYYMMDDHHMMSS, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		programs = append(programs, &model.Program{
			ChannelID:        channelID,
			Title:            programData.Title,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return programs, nil
}
