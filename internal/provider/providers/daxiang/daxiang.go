package daxiang

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type ProgramResponse struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Success  bool   `json:"success"`
	Programs []struct {
		Title     string `json:"title"`
		BeginTime string `json:"beginTime"`
		EndTime   string `json:"endTime"`
	} `json:"programs"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			Name: "河南卫视",
			ID:   "145",
		},
		{
			Name: "河南新闻频道",
			ID:   "149",
		},
		{
			Name: "河南都市频道",
			ID:   "141",
		},
		{
			Name: "河南民生频道",
			ID:   "146",
		},
		{
			Name: "河南法治频道",
			ID:   "147",
		},
		{
			Name: "河南公共频道",
			ID:   "151",
		},
		{
			Name: "河南乡村频道",
			ID:   "152",
		},
		{
			Name: "河南电视剧频道",
			ID:   "148",
		},
		{
			Name: "河南梨园频道",
			ID:   "154",
		},
		{
			Name: "河南文物宝库",
			ID:   "155",
		},
		{
			Name: "河南武术频道",
			ID:   "156",
		},
		{
			Name: "睛彩中原",
			ID:   "157",
		},
		{
			Name: "河南移动戏曲频道",
			ID:   "163",
		},
		{
			Name: "象视界",
			ID:   "183",
		},
		{
			Name: "国学频道",
			ID:   "194",
		},
	}
)

type DaxiangProvider struct {
	*provider.BaseProvider
}

const (
	salt = "6ca114a836ac7d73"
)

func init() {
	provider.Register(provider.Definition{
		ID:         "daxiang",
		BaseURL:    "https://pubmod.hntv.tv",
		Priority:   2,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &DaxiangProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *DaxiangProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "145", "河南卫视", time.Now())

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

func (p *DaxiangProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}
	starOfTheDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location).Unix()

	ts := time.Now().Unix()
	sign := sha256.Sum256(fmt.Appendf(nil, "%s%d", salt, ts))

	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
		"TimeStamp":              fmt.Sprintf("%d", ts),
		"Sign":                   fmt.Sprintf("%x", sign),
	}

	path := fmt.Sprintf("/program/getAuth/vod/originStream/program/%s/%d", providerChannelID, starOfTheDay)

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

func (p *DaxiangProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *DaxiangProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	var result = make([]*model.Program, 0)

	if resp.Code == -2 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.Code), resp.Msg)
	}

	if len(resp.Programs) == 0 {
		return result, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp.Programs {
		st, err := strconv.Atoi(programData.BeginTime)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		et, err := strconv.Atoi(programData.EndTime)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRangeFromTimestamp(int64(st), int64(et), date, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		result = append(result, &model.Program{
			ChannelID:        channelID,
			Title:            programData.Title,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return result, nil
}
