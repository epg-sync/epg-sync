package provider

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type EPGFetcher interface {
	FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error)
	GetID() string
}

type MultiDayEPGFetcher interface {
	EPGFetcher
	FetchEPGMultiDay(ctx context.Context, providerChannelID, channelID string, startDate, endDate time.Time) (map[string][]*model.Program, error)
}

type BaseProvider struct {
	config     *model.ProviderConfig
	httpClient *HTTPClient
	channels   []*model.ProviderChannel
	cache      cache.Cache
	cacheTTL   time.Duration
}

func NewBaseProvider(config *model.ProviderConfig, channels []*model.ProviderChannel) *BaseProvider {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	httpClient := NewHTTPClient(config.BaseURL, timeout)

	return &BaseProvider{
		config:     config,
		httpClient: httpClient,
		channels:   channels,
		cacheTTL:   20 * time.Minute,
	}
}

func (p *BaseProvider) GetID() string {
	return p.config.ID
}

func (p *BaseProvider) GetName() string {
	return p.config.Name
}

func (p *BaseProvider) IsEnabled() bool {
	return p.config.Enabled
}

func (p *BaseProvider) GetPriority() int {
	return p.config.Priority
}

func (p *BaseProvider) SetCache(c cache.Cache) {
	p.cache = c
}

func (p *BaseProvider) GetCache() cache.Cache {
	return p.cache
}

func (p *BaseProvider) Validate() error {
	if p.config.ID == "" {
		return errors.InvalidParam("id", "provider id is required")
	}
	if p.config.Name == "" {
		return errors.InvalidParam("name", "provider name is required")
	}
	return nil
}

func (p *BaseProvider) GetConfig() *model.ProviderConfig {
	return p.config
}

func (p *BaseProvider) GetTimeout() time.Duration {
	if p.config.Timeout <= 0 {
		return 30 * time.Second
	}
	return p.config.Timeout
}

func (p *BaseProvider) GetRetry() int {
	if p.config.MaxRetries <= 0 {
		return 3
	}
	return p.config.MaxRetries
}

func (p *BaseProvider) GetSetting(key string) (any, bool) {
	val, ok := p.config.Settings[key]
	return val, ok
}

func (p *BaseProvider) GetStringSetting(key string) string {
	if val, ok := p.config.Settings[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func (p *BaseProvider) GetIntSetting(key string) int {
	if val, ok := p.config.Settings[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}
	return 0
}

func (p *BaseProvider) GetBoolSetting(key string) bool {
	if val, ok := p.config.Settings[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

func (p *BaseProvider) GetHTTPClient() *HTTPClient {
	return p.httpClient
}

func (p *BaseProvider) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	return p.httpClient.Get(ctx, path, params)
}

func (p *BaseProvider) GetWithHeaders(ctx context.Context, path string, params map[string]string, headers map[string]string) ([]byte, error) {
	return p.httpClient.GetWithHeaders(ctx, path, params, headers)
}

func (p *BaseProvider) PostWithHeaders(ctx context.Context, path string, body io.Reader, headers map[string]string) ([]byte, error) {
	return p.httpClient.PostWithHeaders(ctx, path, body, headers)
}

func (p *BaseProvider) ListChannels() []*model.ProviderChannel {
	return p.channels
}

func (p *BaseProvider) SupportChannel(providerID, channelID string) bool {
	if providerID != p.GetID() {
		return false
	}
	for _, ch := range p.ListChannels() {
		if ch.ID == channelID {
			return true
		}
	}
	return false
}

func (p *BaseProvider) makeCacheKey(providerID, providerChannelID, channelID string, date time.Time) string {
	return fmt.Sprintf("epg:provider_%s:%s_%s_%s",
		providerID,
		providerChannelID,
		channelID,
		date.Format("2006-01-02"),
	)
}

func (p *BaseProvider) getFromCache(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, bool) {
	if p.cache == nil {
		return nil, false
	}

	key := p.makeCacheKey(p.GetID(), providerChannelID, channelID, date)
	var programs []*model.Program
	err := p.cache.Get(ctx, key, &programs)
	if err != nil {
		return nil, false
	}

	return programs, true
}

func (p *BaseProvider) putToCache(ctx context.Context, providerChannelID, channelID string, date time.Time, programs []*model.Program) {
	if p.cache == nil {
		return
	}

	key := p.makeCacheKey(p.GetID(), providerChannelID, channelID, date)

	if err := p.cache.Set(ctx, key, programs, p.cacheTTL); err != nil {
		logger.Error("Failed to set cache",
			logger.String("key", key),
			logger.Err(err),
		)
	}
}

func (p *BaseProvider) putMultiDayToCache(ctx context.Context, providerChannelID, channelID string, programsByDate map[string][]*model.Program) {

	if p.cache == nil {
		return
	}

	for dateStr, programs := range programsByDate {
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			logger.Error("Failed to parse date",
				logger.String("date", dateStr),
				logger.Err(err),
			)
			continue
		}

		p.putToCache(ctx, providerChannelID, channelID, date, programs)
	}
}

func (p *BaseProvider) FetchEPGBatch(ctx context.Context, fetcher EPGFetcher, channelMappingInfo []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {

	logger.Debug("Starting FetchEPGBatch",
		logger.String("provider", fetcher.GetID()),
		logger.Int("channel_count", len(channelMappingInfo)),
		logger.Time("date", date),
	)

	if len(channelMappingInfo) == 0 {
		return nil, nil
	}
	multiDayFetcher, supportsMultiDay := fetcher.(MultiDayEPGFetcher)

	logger.Debug("FetchEPGBatch details",
		logger.String("provider", fetcher.GetID()),
		logger.Bool("supports_multi_day", supportsMultiDay),
	)

	type fetchTask struct {
		channelInfo *model.ChannelMappingInfo
	}

	type fetchResult struct {
		channelInfo *model.ChannelMappingInfo
		programs    []*model.Program
		err         error
	}

	var tasks []fetchTask
	var result []*model.Program

	for _, channelInfo := range channelMappingInfo {
		if cachedPrograms, found := p.getFromCache(ctx, channelInfo.ProviderChannelID, channelInfo.CanonicalID, date); found {
			logger.Debug("Using cached EPG",
				logger.String("provider", fetcher.GetID()),
				logger.String("channel", channelInfo.CanonicalID),
				logger.String("date", date.Format("2006-01-02")),
			)
			result = append(result, cachedPrograms...)
			continue
		}
		tasks = append(tasks, fetchTask{channelInfo: channelInfo})
	}

	if len(tasks) == 0 {
		return result, nil
	}

	maxConcurrency := p.config.RateLimit
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}

	taskChan := make(chan fetchTask, len(tasks))
	resultChan := make(chan fetchResult, len(tasks))

	for _, task := range tasks {
		taskChan <- task
	}
	close(taskChan)

	var wg sync.WaitGroup
	workerCount := min(maxConcurrency, len(tasks))

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				channelInfo := task.channelInfo
				var programs []*model.Program
				var err error

				if supportsMultiDay {
					startDate := date.AddDate(0, 0, -6)
					endDate := date.AddDate(0, 0, 1)
					logger.Debug("Using multi-day EPG fetcher",
						logger.String("provider", fetcher.GetID()),
						logger.String("channel", channelInfo.CanonicalID),
						logger.String("date_range", fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))),
					)
					programsByDate, fetchErr := multiDayFetcher.FetchEPGMultiDay(
						ctx,
						channelInfo.ProviderChannelID,
						channelInfo.CanonicalID,
						startDate,
						endDate,
					)

					if fetchErr == nil {
						logger.Debug("Fetched multi-day EPG and putting to cache",
							logger.String("provider", fetcher.GetID()),
							logger.String("channel", channelInfo.CanonicalID),
							logger.String("date_range", fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))),
						)
						p.putMultiDayToCache(ctx, channelInfo.ProviderChannelID, channelInfo.CanonicalID, programsByDate)
						dateStr := date.Format("2006-01-02")
						programs = programsByDate[dateStr]
					} else {
						err = fetchErr
					}
				} else {
					programs, err = fetcher.FetchEPG(ctx, channelInfo.ProviderChannelID, channelInfo.CanonicalID, date)
					if err == nil && programs != nil {
						p.putToCache(ctx, channelInfo.ProviderChannelID, channelInfo.CanonicalID, date, programs)
					}
				}

				resultChan <- fetchResult{
					channelInfo: channelInfo,
					programs:    programs,
					err:         err,
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var firstErr error
	for res := range resultChan {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
				logger.Error("Failed to fetch EPG",
					logger.String("provider", fetcher.GetID()),
					logger.String("channel", res.channelInfo.CanonicalID),
					logger.Err(res.err),
				)
			}
			continue
		}
		result = append(result, res.programs...)
	}

	if firstErr != nil {
		return result, firstErr
	}

	return result, nil
}

func (p *BaseProvider) ProcessProgramTimeRange(startTime, endTime, date, layout string, location *time.Location) (time.Time, time.Time, error) {

	st, err := time.ParseInLocation(layout, startTime, location)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse start time: %w", err)
	}
	et, err := time.ParseInLocation(layout, endTime, location)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse end time: %w", err)
	}

	return p.validateTimeRange(st, et, date, location)
}

func (p *BaseProvider) ProcessProgramTimeRangeFromTimestamp(startTimestamp, endTimestamp int64, date string, location *time.Location) (time.Time, time.Time, error) {
	st := parseTimestamp(startTimestamp)
	et := parseTimestamp(endTimestamp)

	st = st.In(location)
	et = et.In(location)

	return p.validateTimeRange(st, et, date, location)
}

func parseTimestamp(timestamp int64) time.Time {
	if timestamp > 9999999999 {
		return time.UnixMilli(timestamp)
	}
	return time.Unix(timestamp, 0)
}

func (p *BaseProvider) validateTimeRange(st, et time.Time, date string, location *time.Location) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse date: %w", err)
	}
	startOfDay := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, location)
	endOfDay := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, location)
	if st.Before(startOfDay) {
		st = startOfDay
	}

	if et.After(endOfDay) {
		et = endOfDay
	}

	return st, et, nil
}
