package hebei

import (
	"bytes"
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
	State   int    `json:"state"`
	Message string `json:"message"`
	Success bool   `json:"success"`
	Data    map[string][]struct {
		StartDateTime string `json:"startDateTime"`
		EndDateTime   string `json:"endDateTime"`
		Name          string `json:"Name"`
	} `json:"Data"`
}

var (
	channelList = []*model.ProviderChannel{
		{
			Name: "河北卫视",
			ID:   "462",
		},
		{
			Name: "河北经济生活",
			ID:   "114",
		},
		{
			Name: "河北三农",
			ID:   "118",
		},
		{
			Name: "河北都市",
			ID:   "62",
		},
		{
			Name: "河北影视剧",
			ID:   "334",
		},
		{
			Name: "河北少儿科教",
			ID:   "70",
		},
		{
			Name: "河北文旅公共",
			ID:   "338",
		},
	}
)

const tenantID = "0d91d6cfb98f5b206ac1e752757fc5a9"

type HebeiProvider struct {
	*provider.BaseProvider
}

func init() {
	provider.Register("hebei", New)
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &HebeiProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *HebeiProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
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

func (p *HebeiProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
		"Tenantid":               tenantID,
	}
	formatDate := date.Format("2006-01-02")
	reqBody := map[string]string{
		"day":      formatDate,
		"dayEnd":   formatDate,
		"sourceId": providerChannelID,
		"tenantId": tenantID,
	}

	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	res, err := p.PostWithHeaders(ctx, "/spidercrms/api/live/liveShowSet/findNoPage", bytes.NewReader(reqBodyJSON), headers)

	if err != nil {
		return nil, err
	}

	programData, err := p.ParseEPGResponse(res, providerChannelID, channelID, formatDate)
	if err != nil {
		return nil, err
	}
	return programData, nil
}

func (p *HebeiProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *HebeiProvider) ParseEPGResponse(data []byte, providerChannelID, channelID, date string) ([]*model.Program, error) {
	var resp ProgramResponse
	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}
	var result = make([]*model.Program, 0)

	if resp.State != 200 {
		return nil, errors.ProviderAPIError(p.GetID(), fmt.Sprintf("%d", resp.State), resp.Message)
	}

	location, err := time.LoadLocation(provider.UTC8Location)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, err
	}

	for _, program := range resp.Data[date] {

		startTime, err := time.ParseInLocation("2006-01-02 15:04:05", program.StartDateTime, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}

		endTime, err := time.ParseInLocation("2006-01-02 15:04:05", program.EndDateTime, location)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}

		result = append(result, &model.Program{
			ChannelID:        channelID,
			Title:            program.Name,
			StartTime:        startTime,
			EndTime:          endTime,
			OriginalTimezone: provider.UTC8Location,
			ProviderID:       p.GetID(),
		})
	}
	return result, nil
}
