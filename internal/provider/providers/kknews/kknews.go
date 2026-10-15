package kknews

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type KKNewsProvider struct {
	*provider.BaseProvider
}

type ProgramResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Result  struct {
		Programs []struct {
			Name            string `json:"name"`
			StartTimeString string `json:"start_time_string"`
			EndTimeString   string `json:"end_time_string"`
		}
	} `json:"result"`
}

var (
	referer     = "https://live.kankanews.com/"
	apiVersion  = "v1"
	version     = "2.32.2"
	salt        = "28c8edde3d61a0411511d3b1866f0636"
	platform    = "pc"
	channelList = []*model.ProviderChannel{
		{
			ID:   "1",
			Name: "东方卫视",
		},
		{
			ID:   "2",
			Name: "上海新闻综合",
		},
		{
			ID:   "5",
			Name: "第一财经",
		},
		{
			ID:   "10",
			Name: "五星体育",
		},
		{
			ID:   "4",
			Name: "上海都市",
		},
		{
			ID:   "9",
			Name: "哈哈炫动",
		},
	}
)

func init() {
	provider.Register(provider.Definition{
		ID:         "kknews",
		BaseURL:    "https://kapi.kankanews.com",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &KKNewsProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *KKNewsProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "1", "东方卫视", time.Now())

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

func (p *KKNewsProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	dataStr := date.Format("2006-01-02")
	timestamp := time.Now().Unix()
	nonce := p.generateNonce()
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
		provider.HeaderReferer:   referer,
		provider.HeaderOrigin:    referer,
		"timestamp":              fmt.Sprintf("%d", timestamp),
		"nonce":                  nonce,
		"sign":                   p.generateSign(providerChannelID, dataStr, timestamp, nonce),
		"platform":               platform,
		"api-version":            apiVersion,
		"version":                version,
	}
	res, err := p.GetWithHeaders(ctx, "/content/pc/tv/programs", map[string]string{
		"date":       dataStr,
		"channel_id": providerChannelID,
	}, headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID, dataStr)

	if err != nil {
		return nil, err
	}

	return programData, nil
}

func (p *KKNewsProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *KKNewsProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, err
	}

	if resp.Code != "1000" {
		return nil, errors.ProviderAPIError(p.GetID(), resp.Code, resp.Message)
	}

	var programs []*model.Program

	if len(resp.Result.Programs) == 0 {
		return programs, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	for _, programData := range resp.Result.Programs {
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRange(programData.StartTimeString, programData.EndTimeString, date, provider.TimeLayoutYYYYMMDDHHMMSS, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		programs = append(programs, &model.Program{
			ChannelID:        channelID,
			Title:            programData.Name,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return programs, nil
}

func (p *KKNewsProvider) generateSign(channelID, date string, timeStamp int64, nonce string) string {
	signStr := fmt.Sprintf("Api-Version=%s&channel_id=%s&date=%s&nonce=%s&platform=%s&timestamp=%d&version=%s&%s", apiVersion, channelID, date, nonce, platform, timeStamp, version, salt)
	sign := fmt.Sprintf("%x", md5.Sum([]byte(signStr)))
	return fmt.Sprintf("%x", md5.Sum([]byte(sign)))
}

func (p *KKNewsProvider) generateNonce() string {
	const charset = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}
