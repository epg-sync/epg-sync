package jstv

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/epg-sync/epgsync/pkg/utils"
)

type ProgramResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Epg []struct {
			Date string `json:"date"`
			Data []struct {
				ProgramName string `json:"programName"`
				StartTime   string `json:"startTime"`
				EndTime     string `json:"endTime"`
			} `json:"data"`
		} `json:"epg"`
	} `json:"data"`
}

type AccessesTokenResponse struct {
	Data struct {
		AccessToken string `json:"accessToken"`
		Code        int    `json:"code"`
		Message     string `json:"message"`
	}
}

func init() {
	provider.Register(provider.Definition{
		ID:         "jstv",
		BaseURL:    "https://live-lizhi.jstv.com",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

type JSTVProvider struct {
	*provider.BaseProvider
}

var channelList = []*model.ProviderChannel{
	{
		ID:   "670",
		Name: "江苏卫视",
	},
	{
		ID:   "669",
		Name: "江苏城市",
	},
	{
		ID:   "663",
		Name: "江苏综艺",
	},
	{
		ID:   "664",
		Name: "江苏影视",
	},
	{
		ID:   "668",
		Name: "江苏新闻",
	},
	{
		ID:   "666",
		Name: "江苏教育",
	},
	{
		ID:   "665",
		Name: "江苏体育休闲",
	},
	{
		ID:   "667",
		Name: "优漫卡通",
	},
	{
		ID:   "671",
		Name: "江苏国际",
	},
}

const (
	appID     = "3b93c452b851431c8b3a076789ab1e14"
	appSecret = "9dd4b0400f6e4d558f2b3497d734c2b4"
	platform  = "41"
)

func New(config *model.ProviderConfig) (provider.Provider, error) {

	return &JSTVProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *JSTVProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "670", "江苏卫视", time.Now())

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

func (p *JSTVProvider) FetchEPGMultiDay(ctx context.Context, providerChannelID, channelID string, startDate, endDate time.Time) (map[string][]*model.Program, error) {
	accessToken, err := p.GetAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %v", err)
	}
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
		"Authorization":          fmt.Sprintf("Bearer %s", accessToken),
	}
	res, err := p.GetWithHeaders(ctx, "/api/Channel/Epg", map[string]string{
		"channelId":      providerChannelID,
		"days":           "6",
		"isNeedTomorrow": "1",
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
func (p *JSTVProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, []*model.ChannelMappingInfo{
		{
			ProviderChannelID: providerChannelID,
			CanonicalID:       channelID,
		},
	}, date)
}
func (p *JSTVProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *JSTVProvider) ParseEPGResponse(data []byte, providerChannelID, channelID string) (map[string][]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	if resp.Code != 200 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.Code), resp.Message)
	}

	programs := make(map[string][]*model.Program)

	if len(resp.Data.Epg) == 0 {
		return programs, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, epg := range resp.Data.Epg {
		for _, programData := range epg.Data {
			startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRange(programData.StartTime, programData.EndTime, epg.Date, provider.TimeLayoutYYYYMMDDHHMMSS, location)
			if err != nil {
				logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, epg.Date).Error())
				continue
			}
			programs[epg.Date] = append(programs[epg.Date], &model.Program{
				ChannelID:        channelID,
				Title:            programData.ProgramName,
				StartTime:        startTime,
				EndTime:          endTime,
				OriginalTimezone: provider.UTC8Location,
				ProviderID:       p.GetID(),
			})
		}
	}

	return programs, nil
}

func (p *JSTVProvider) FetchAccessToken(ctx context.Context) (string, error) {
	uuid, err := utils.GenerateRandomString(32)
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/JwtAuth/GetWebToken?AppID=%s", appID)
	params := fmt.Sprintf("appId%splatform%suuid%s", appID, platform, uuid)
	tm := time.Now().Unix()
	signatureRaw := fmt.Sprintf("%s%s%s%d", appSecret, path, params, tm)
	sign := fmt.Sprintf("%x", md5.Sum([]byte(signatureRaw)))
	reqURL := fmt.Sprintf("https://api-auth-lizhi.jstv.com%s&TT=%s&Sign=%s", path, calculateTimeStamp(tm), sign)
	reqBody := map[string]string{
		"appId":    appID,
		"platform": platform,
		"uuid":     uuid,
	}

	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	res, err := p.PostWithHeaders(ctx, reqURL, bytes.NewReader(reqBodyJSON), nil)
	if err != nil {
		return "", err
	}

	var accessTokenResp AccessesTokenResponse
	err = json.Unmarshal(res, &accessTokenResp)
	if err != nil {
		return "", err
	}

	if accessTokenResp.Data.Code != 0 {
		return "", errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", accessTokenResp.Data.Code), accessTokenResp.Data.Message)
	}

	return accessTokenResp.Data.AccessToken, nil
}

func (p *JSTVProvider) GetAccessToken(ctx context.Context) (string, error) {
	var accessToken string
	err := p.GetCache().Get(ctx, "jstv_access_token", &accessToken)

	if err == nil && accessToken != "" {
		return accessToken, nil
	}

	accessToken, err = p.FetchAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch access token: %w", err)
	}

	err = p.GetCache().Set(ctx, "jstv_access_token", accessToken, 1*time.Hour)
	if err != nil {
		logger.Error("Failed to cache access token", logger.Err(err))
	}

	return accessToken, nil
}

func calculateTimeStamp(tm int64) string {
	t := [4]int64{255 & tm, (65280 & tm) >> 8, (16711680 & tm) >> 16, (4278190080 & tm) >> 24}
	for n := range len(t) {
		t[n] = ((240 & t[n]) ^ 240) | ((1 + (15 & t[n])) & 15)
	}
	finalInt32 := int32(t[3]) | (int32(t[2]) << 8) | (int32(t[1]) << 16) | (int32(t[0]) << 24)

	return fmt.Sprintf("%d", finalInt32)
}
