package bfgd

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
	Total     int `json:"total"`
	EventList []struct {
		EventName string `json:"event_name"`
		StartTime int64  `json:"start_time"`
		EndTime   int64  `json:"end_time"`
	} `json:"event_list"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "4200000635",
			Name: "重温经典",
		},
	}
)

type XiamenProvider struct {
	*provider.BaseProvider
}

var accessToken = "R621C86FCU319FA04BK783FB5EBIFA29A0DEP2BF4M340CAC5V0Z339C9W16D7E5AFCA1ADFD1"

func init() {
	provider.Register(provider.Definition{
		ID:         "bfgd",
		BaseURL:    "http://slave.bfgd.com.cn",
		Priority:   1,
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
	_, err := p.FetchEPG(ctx, "4200000635", "经典重温", time.Now())

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

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	startTime := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	endTime := startTime.Add(24 * time.Hour)

	res, err := p.GetWithHeaders(ctx, "/media/event/get_list", map[string]string{
		"chnlid":      providerChannelID,
		"pageidx":     "1",
		"vcontrol":    "0",
		"attachdesc":  "0",
		"repeat":      "0",
		"pagenum":     "2048",
		"flagposter":  "0",
		"accesstoken": accessToken,
		"starttime":   fmt.Sprintf("%d", startTime.Unix()),
		"endtime":     fmt.Sprintf("%d", endTime.Unix()),
	}, headers)

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

	if resp.Total == 0 {
		return result, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp.EventList {
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRangeFromTimestamp(programData.StartTime, programData.EndTime, date, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		result = append(result, &model.Program{
			ChannelID:        channelID,
			Title:            programData.EventName,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return result, nil
}
