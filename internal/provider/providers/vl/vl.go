// Package vl 抓取緯來電視網官方节目表（手机版页面 m.vl.com.tw）。
//
// 页面按「播出日」组织：wdt=D 的页面从 D 日 07:00 开始，一直列到 D+1 日清晨（00:00~06:59 的节目
// 也在这一页，时间只写 HH:MM）。所以日历上 D 日的节目分布在两页里：
// 00:00~07:00 在 D-1 的页面末尾，07:00~24:00 在 D 的页面。
package vl

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type VLProvider struct {
	*provider.BaseProvider
}

const (
	listPath = "/program_list_320.asp"
	timezone = "Asia/Taipei"
	// 每页节目从这个时间开始，也是上一页最后一个节目的结束时间
	broadcastDayStart = 7 * time.Hour
)

var (
	// ID 是页面上频道下拉框的值（参数 kd）
	channelList = []*model.ProviderChannel{
		{
			ID:      "SPT",
			Name:    "緯來體育",
			Aliases: []string{"纬来体育"},
		},
	}

	// 一行节目：第一个单元格是 HH:MM，第二个是节目名，其中 <font size=2> 里是集数或对阵等副标题
	// （通常前面有 <br/>，节目异动的行没有）。
	// 正在播出的那行 class 是 black28，其余是 gray28，所以不按 class 匹配
	rowPattern  = regexp.MustCompile(`(?s)<td[^>]*>\s*(\d{1,2}):(\d{2})\s*</td>\s*<td[^>]*>(.*?)</td>`)
	fontPattern = regexp.MustCompile(`(?is)<font[^>]*>(.*?)</font>`)
	tagPattern  = regexp.MustCompile(`(?s)<[^>]*>`)
)

func init() {
	provider.Register(provider.Definition{
		ID:         "vl",
		Name:       "緯來電視網",
		BaseURL:    "https://m.vl.com.tw",
		Priority:   1,
		Timeout:    10 * time.Second,
		RateLimit:  2,
		MaxRetries: 2,
		New:        New,
	})
}

func New(config *model.ProviderConfig) (provider.Provider, error) {
	return &VLProvider{
		BaseProvider: provider.NewBaseProvider(config, channelList),
	}, nil
}

func (p *VLProvider) HealthCheck(ctx context.Context) *model.ProviderHealth {
	if _, err := p.fetchPage(ctx, channelList[0].ID, time.Now()); err != nil {
		return &model.ProviderHealth{
			Healthy: false,
			Message: fmt.Sprintf("fetch program list failed: %v", err),
		}
	}
	return &model.ProviderHealth{Healthy: true, Message: "OK"}
}

func (p *VLProvider) FetchEPGBatch(ctx context.Context, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return p.BaseProvider.FetchEPGBatch(ctx, p, channelMappingInfo, date)
}

// FetchEPG 返回台湾时间 date 当天 00:00~24:00 开始的节目，需要 date 前一天和当天两页
func (p *VLProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, errors.ProviderParseFailed(p.GetID(), err)
	}
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)

	var entries []entry
	for _, pageDay := range []time.Time{day.AddDate(0, 0, -1), day} {
		body, err := p.fetchPage(ctx, providerChannelID, pageDay)
		if err != nil {
			// 前一天的页面只提供凌晨的节目，取不到时仍返回当天 07:00 以后的部分
			if pageDay.Before(day) {
				logger.Warn("vl: failed to fetch previous broadcast day, early-morning programs will be missing",
					logger.String("channel_id", channelID),
					logger.String("page", pageDay.Format("2006-01-02")),
					logger.Err(err),
				)
				continue
			}
			return nil, err
		}
		entries = append(entries, parsePage(body, pageDay)...)
	}

	programs := buildPrograms(entries, day, channelID, p.GetID())
	if len(programs) == 0 {
		return nil, errors.EPGNotFound(channelID, day.Format("2006-01-02"))
	}
	return programs, nil
}

func (p *VLProvider) fetchPage(ctx context.Context, kd string, day time.Time) ([]byte, error) {
	// udt 是该周的周一，页面上的周切换链接都是这样带的
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	params := map[string]string{
		"udt": monday.Format("2006/01/02"),
		"wdt": day.Format("2006/01/02"),
		"kd":  kd,
	}
	headers := map[string]string{
		provider.HeaderUserAgent: provider.DefaultUserAgent,
		provider.HeaderReferer:   "https://m.vl.com.tw" + listPath,
	}
	return p.GetWithHeaders(ctx, listPath, params, headers)
}

// entry 是页面上的一行，start 已换算成绝对时间，end 由下一行的开始时间决定
type entry struct {
	start    time.Time
	title    string
	subtitle string
	// 所在播出日的结束（下一页的开始），用作该页最后一个节目的结束时间
	pageEnd time.Time
}

// parsePage 解析 pageDay 的页面。行按时间顺序排列，时间变小说明过了午夜，日期加一天
func parsePage(body []byte, pageDay time.Time) []entry {
	pageEnd := pageDay.AddDate(0, 0, 1).Add(broadcastDayStart)

	var (
		entries []entry
		day                   = pageDay
		prev    time.Duration = -1
	)
	for _, m := range rowPattern.FindAllSubmatch(body, -1) {
		var hour, minute int
		fmt.Sscanf(string(m[1]), "%d", &hour)
		fmt.Sscanf(string(m[2]), "%d", &minute)
		offset := time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute
		if offset < prev {
			day = day.AddDate(0, 0, 1)
		}
		prev = offset

		title, subtitle := splitCell(string(m[3]))
		if title == "" {
			continue
		}
		entries = append(entries, entry{
			start:    day.Add(offset),
			title:    title,
			subtitle: subtitle,
			pageEnd:  pageEnd,
		})
	}
	return entries
}

// splitCell 把节目单元格拆成节目名和副标题（<font> 中的内容），去掉标签和多余空白
func splitCell(cell string) (string, string) {
	var subtitle string
	if m := fontPattern.FindStringSubmatch(cell); m != nil {
		subtitle = cleanText(m[1])
		cell = strings.Replace(cell, m[0], " ", 1)
	}
	return cleanText(cell), subtitle
}

func cleanText(s string) string {
	s = html.UnescapeString(tagPattern.ReplaceAllString(s, " "))
	return strings.Join(strings.Fields(s), " ")
}

// buildPrograms 按开始时间排序，用下一行的开始时间作为结束时间，只保留在 day 当天开始的节目
func buildPrograms(entries []entry, day time.Time, channelID, providerID string) []*model.Program {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].start.Before(entries[j].start) })

	dayEnd := day.AddDate(0, 0, 1)
	var programs []*model.Program
	for i, e := range entries {
		if e.start.Before(day) || !e.start.Before(dayEnd) {
			continue
		}
		end := e.pageEnd
		if i+1 < len(entries) && entries[i+1].start.Before(end) {
			end = entries[i+1].start
		}
		if !end.After(e.start) {
			continue
		}
		programs = append(programs, &model.Program{
			ChannelID:        channelID,
			Title:            e.title,
			Description:      e.subtitle,
			StartTime:        e.start,
			EndTime:          end,
			OriginalTimezone: timezone,
			ProviderID:       providerID,
		})
	}
	return programs
}
