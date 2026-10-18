package iqilu

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type ProgramResponse struct {
	Code   int    `json:"code"`
	ErrMsg string `json:"msg"`
	Data   struct {
		Infos []Program `json:"infos"`
	} `json:"data"`
}

type Program struct {
	StartTime string `json:"start_time"`
	BeginTime int64  `json:"begintime"`
	EndTime   string `json:"end_time"`
	Name      string `json:"-"`
	NameRaw   any    `json:"name"` // some title contain non-string types
}

type IQiluProvider struct {
	*provider.BaseProvider
}

var (
	channelList = []*model.ProviderChannel{
		{
			ID:   "24",
			Name: "山东卫视",
		},
		{
			ID:   "25",
			Name: "山东齐鲁",
		},
		{
			ID:   "26",
			Name: "山东体育",
		},
		{
			ID:   "29",
			Name: "山东生活",
		},
		{
			ID:   "28",
			Name: "山东综艺",
		},
		{
			ID:   "31",
			Name: "山东新闻",
		},
		{
			ID:   "30",
			Name: "山东农科",
		},
		{
			ID:   "32",
			Name: "山东少儿",
		},
	}
)

func init() {
	provider.Register(provider.Definition{
		ID:         "iqilu",
		BaseURL:    "https://sdxw.iqilu.com",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &IQiluProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *IQiluProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "24", "山东卫视", time.Now())

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

func (p *IQiluProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
	}
	formatDate := date.Format("2006-01-02")
	res, err := p.GetWithHeaders(ctx, "/v1/app/play/program/qilu", map[string]string{
		"channelID": providerChannelID,
		"date":      formatDate,
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

func (p *IQiluProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *IQiluProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}
	if resp.Code != 1 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.Code), resp.ErrMsg)
	}
	var programs []*model.Program

	if len(resp.Data.Infos) == 0 {
		return programs, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp.Data.Infos {
		startClock, startErr := normalizeClock(programData.StartTime)
		endClock, endErr := normalizeClock(programData.EndTime)
		if err := cmp.Or(startErr, endErr); err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}

		startTimeParsed, endTimeParsed, err := p.BaseProvider.ProcessProgramTimeRange(
			date+" "+startClock, date+" "+endClock, date, provider.TimeLayoutYYYYMMDDHHMMSS, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}

		if endTimeParsed.Before(startTimeParsed) || endTimeParsed.Equal(startTimeParsed) {
			continue
		}

		programs = append(programs, &model.Program{
			ChannelID:        channelID,
			Title:            strings.TrimSpace(programData.Name),
			StartTime:        startTimeParsed,
			EndTime:          endTimeParsed,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return programs, nil
}

// normalizeClock 把接口返回的时刻统一成 HH:MM:SS。
// 不同频道、不同日期的格式不一致：多数是 "06:30:00"，也有 "6:30"（2026-10-18 山东农科），
// 还出现过全角冒号。"24:00" 表示当天结束，换成 23:59:59（结束时间本来也会被截到当天）
func normalizeClock(s string) (string, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "：", ":"))
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return "", fmt.Errorf("unrecognized time %q", s)
	}
	nums := make([]int, 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || len(part) > 2 {
			return "", fmt.Errorf("unrecognized time %q", s)
		}
		nums[i] = n
	}
	h, m, sec := nums[0], nums[1], nums[2]
	if h == 24 && m == 0 && sec == 0 {
		return "23:59:59", nil
	}
	if h > 23 || m > 59 || sec > 59 {
		return "", fmt.Errorf("time out of range %q", s)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, sec), nil
}

func (p *Program) UnmarshalJSON(data []byte) error {
	type Alias Program
	aux := &struct {
		NameRaw any `json:"name"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	switch v := aux.NameRaw.(type) {
	case string:
		p.Name = v
	case float64:
		p.Name = fmt.Sprintf("%.0f", v)
	}

	return nil
}
