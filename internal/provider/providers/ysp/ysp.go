package ysp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
	"google.golang.org/protobuf/proto"
)

type YSPProvider struct {
	*provider.BaseProvider
}

var (
	referer = "https://www.yangshipin.cn/"
)

func init() {
	provider.Register(provider.Definition{
		ID:         "ysp",
		BaseURL:    "https://capi.yangshipin.cn",
		Priority:   2,
		Timeout:    10 * time.Second,
		RateLimit:  10,
		MaxRetries: 3,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &YSPProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *YSPProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	_, err := p.FetchEPG(ctx, "600001859", "CCTV1", time.Now())

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

func (p *YSPProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
		provider.HeaderReferer:   referer,
	}
	path := fmt.Sprintf("/api/yspepg/program/%s/%s", providerChannelID, date.Format("20060102"))
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

func (p *YSPProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *YSPProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp CnYangshipinOmstvCommonProtoEpgProgramModel_Response
	err := proto.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	if resp.GetCode() != 200 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.GetCode()), resp.GetMessage())
	}

	var programs []*model.Program

	if len(resp.DataList) == 0 {
		return programs, nil
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, programData := range resp.DataList {
		startTime, endTime, err := p.BaseProvider.ProcessProgramTimeRangeFromTimestamp(int64(programData.St), int64(programData.Et), date, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}
		programName := strings.ReplaceAll(programData.Name, "（版权原因不可回看）", " ")
		programs = append(programs, &model.Program{
			ChannelID:        channelID,
			Title:            programName,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}

	return programs, nil
}
