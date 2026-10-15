// 江西今视频
package jsp

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

type ProgramResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Result  []struct {
		ProgramName string `json:"programName"`
		PlayTime    string `json:"playTime"`
	} `json:"result"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "87",
			Name: "江西卫视",
		},
		{
			ID:   "86",
			Name: "江西都市",
		},
		{
			ID:   "153",
			Name: "江西经济生活",
		},
		{
			ID:   "83",
			Name: "江西公共农业",
		},
		{
			ID:   "82",
			Name: "江西少儿",
		},
		{
			ID:   "81",
			Name: "江西新闻",
		},
	}
)

type JspProvider struct {
	*provider.BaseProvider
}

var userAgent = "GVideo/5.10.04 (com.sobey.JiangXiTV; build:5.10.031; iOS 26.1.0) Alamofire/5.7.1"

func init() {
	provider.Register(provider.Definition{
		ID:         "jsp",
		BaseURL:    "https://app.jxgdw.com",
		Priority:   2,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &JspProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *JspProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, channelList[0].ID, channelList[0].Name, time.Now())

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

func (p *JspProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: userAgent,
	}
	formatDate := date.Format("2006-01-02")
	path := fmt.Sprintf("/api/tv/channel/%s/menus", providerChannelID)
	res, err := p.GetWithHeaders(ctx, path, map[string]string{
		"playDate": formatDate,
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

func (p *JspProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *JspProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}
	var result = make([]*model.Program, 0)

	if resp.Code != 0 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.Code), resp.Message)
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	var nextEndTime time.Time

	for i := len(resp.Result) - 1; i >= 0; i-- {
		programData := resp.Result[i]

		startTime, err := time.ParseInLocation("2006-01-02 15:04:05", programData.PlayTime, location)
		if err != nil {
			logger.Error("Failed to parse event time", logger.Err(err))
			continue
		}
		if nextEndTime.IsZero() {
			nextEndTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 23, 59, 59, 0, location).Add(24 * time.Hour)
		}

		result = append(result, &model.Program{
			ChannelID:        channelID,
			Title:            programData.ProgramName,
			StartTime:        startTime,
			EndTime:          nextEndTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}
	slices.Reverse(result)
	return result, nil
}
