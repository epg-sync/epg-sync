package cctv

import (
	"context"
	"encoding/json"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type CCTVEPGResponse struct {
	Errcode string `json:"errcode"`
	Msg     string `json:"msg"`
	Data    map[string]struct {
		List []struct {
			Title     string `json:"title"`
			StartTime int64  `json:"startTime"`
			EndTime   int64  `json:"endTime"`
		}
	}
}

func init() {
	provider.Register(provider.Definition{
		ID:         "cctv_cn",
		Name:       "cctv.cn",
		BaseURL:    "https://zy.api.cntv.cn",
		Priority:   3,
		Timeout:    10 * time.Second,
		RateLimit:  10,
		MaxRetries: 3,
		New:        New,
	})
}

type CCTVProvider struct {
	*provider.BaseProvider
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &CCTVProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

var (
	referer = "https://tv.cctv.com/"
)

func (p *CCTVProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "cctv1", "CCTV1", time.Now())
	if err != nil {
		return &model.ProviderHealth{
			Healthy: false,
			Message: err.Error(),
		}
	}

	return &model.ProviderHealth{
		Healthy: true,
		Message: "OK",
	}
}

func (p *CCTVProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
		provider.HeaderReferer:   referer,
	}
	formatDate := date.Format("20060102")
	reqParams := map[string]string{
		"c":         providerChannelID,
		"serviceId": "tvcctv",
		"d":         formatDate,
	}

	res, err := p.GetWithHeaders(ctx, "/epg/getEpgInfoByChannelNew", reqParams, headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID, date.Format("2006-01-02"))

	if err != nil {
		return nil, err
	}

	return programData, nil
}

func (p *CCTVProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *CCTVProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp CCTVEPGResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, err
	}

	if resp.Errcode != "" {
		return nil, errors.ProviderAPIError(p.GetID(), resp.Errcode, resp.Msg)
	}

	var programs []*model.Program
	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp.Data[providerChannelID].List {
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRangeFromTimestamp(programData.StartTime, programData.EndTime, date, location)
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
