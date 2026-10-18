package homeplus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type HomeplusProvider struct {
	*provider.BaseProvider
}

type programItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	BeginTime   string `json:"beginTime"`
	EndTime     string `json:"endTime"`
}

// date_program 每天的值是 [[...], {"9": {...}, "10": {...}}]，
// 第一段是数组，后续段是以序号为 key 的对象，需要按序号合并
type ProgramResponse struct {
	DateProgram map[string][]json.RawMessage `json:"date_program"`
	Date        []string                     `json:"date"`
}

const (
	referer  = "https://www.homeplus.net.tw/cable/product-introduce/digital-tv/digital-program-cont/"
	origin   = "https://www.homeplus.net.tw"
	apiPath  = "/cable/Product_introduce/digital_tv/get_channel_content"
	timezone = provider.UTC8Location // 台湾时间同为 UTC+8
)

var (
	// ID 格式为 "{so}-{channelid}"，与节目页 URL digital-program-cont/{so}-{channelid} 一致
	channelList = []*model.ProviderChannel{
		{
			ID:      "720-72",
			Name:    "緯來體育",
			Aliases: []string{"纬来体育"},
		},
	}
)

// 節目表与实际播出有出入，优先级放到最低，只作后备：緯來體育优先用官方的 vl
func init() {
	provider.Register(provider.Definition{
		ID:         "homeplus",
		BaseURL:    "https://www.homeplus.net.tw",
		Priority:   5,
		Timeout:    10 * time.Second,
		RateLimit:  5,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &HomeplusProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *HomeplusProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
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

func (p *HomeplusProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	programsByDate, err := p.FetchEPGMultiDay(ctx, providerChannelID, channelID, date, date)
	if err != nil {
		return nil, err
	}
	return programsByDate[date.Format("2006-01-02")], nil
}

// 接口一次返回前一天起共 8 天的节目单，忽略 start/end
func (p *HomeplusProvider) FetchEPGMultiDay(ctx context.Context, providerChannelID, channelID string, start, end time.Time) (map[string][]*model.Program, error) {
	so, cid, ok := strings.Cut(providerChannelID, "-")
	if !ok {
		return nil, errors.InvalidParam("providerChannelID", fmt.Sprintf("expected format {so}-{channelid}, got %q", providerChannelID))
	}

	logger.Debug("FetchEPGMultiDay time range", logger.Time("start", start), logger.Time("end", end))

	headers := map[string]string{
		provider.HeaderUserAgent:   provider.DefaultUserAgent,
		provider.HeaderContentType: "application/x-www-form-urlencoded; charset=UTF-8",
		provider.HeaderOrigin:      origin,
		provider.HeaderReferer:     referer + providerChannelID,
		"X-Requested-With":         "XMLHttpRequest",
	}
	form := url.Values{
		"so":        {so},
		"channelid": {cid},
	}

	res, err := p.PostWithHeaders(ctx, apiPath, strings.NewReader(form.Encode()), headers)
	if err != nil {
		return nil, err
	}

	return p.ParseEPGResponse(res, providerChannelID, channelID)
}

func (p *HomeplusProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

func (p *HomeplusProvider) ParseEPGResponse(data []byte, providerChannelID, channelID string) (map[string][]*model.Program, error) {
	var resp ProgramResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		logger.Warn(errors.ErrProgramLoadLocation(channelID, err).Error())
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}

	result := make(map[string][]*model.Program, len(resp.DateProgram))
	for date, chunks := range resp.DateProgram {
		items, err := flattenChunks(chunks)
		if err != nil {
			logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
			continue
		}

		programs := make([]*model.Program, 0, len(items))
		for _, item := range items {
			startTime, endTime, err := p.parseTimeRange(item.BeginTime, item.EndTime, date, location)
			if err != nil {
				logger.Warn(errors.ErrProgramDateRangeProcess(err, channelID, date).Error())
				continue
			}
			programs = append(programs, &model.Program{
				ChannelID:        channelID,
				Title:            item.Name,
				Description:      item.Description,
				StartTime:        startTime,
				EndTime:          endTime,
				OriginalTimezone: timezone,
				ProviderID:       p.GetID(),
			})
		}
		result[date] = programs
	}

	return result, nil
}

// 时间只有 HH:MM，结束时间早于等于开始时间说明跨天（如 22:15-01:15、06:30-00:00）
func (p *HomeplusProvider) parseTimeRange(begin, end, date string, location *time.Location) (time.Time, time.Time, error) {
	const layout = "2006-01-02 15:04"
	endDate := date
	if end <= begin {
		d, err := time.ParseInLocation("2006-01-02", date, location)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		endDate = d.AddDate(0, 0, 1).Format("2006-01-02")
	}
	return p.BaseProvider.ProcessProgramTimeRange(date+" "+begin, endDate+" "+end, date, layout, location)
}

func flattenChunks(chunks []json.RawMessage) ([]programItem, error) {
	var items []programItem
	for _, chunk := range chunks {
		var list []programItem
		if err := json.Unmarshal(chunk, &list); err == nil {
			items = append(items, list...)
			continue
		}

		var indexed map[string]programItem
		if err := json.Unmarshal(chunk, &indexed); err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(indexed))
		for k := range indexed {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, _ := strconv.Atoi(keys[i])
			b, _ := strconv.Atoi(keys[j])
			return a < b
		})
		for _, k := range keys {
			items = append(items, indexed[k])
		}
	}
	return items, nil
}
