package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/robfig/cron/v3"
)

type SchedulerService struct {
	cron                  *cron.Cron
	epgService            *EPGService
	cache                 cache.Cache
	channelService        *ChannelService
	channelMappingService *ChannelMappingService
	chain                 *provider.Chain
	customSources         *CustomSourceService
	mu                    sync.RWMutex
	jobs                  map[string]cron.EntryID

	// syncMu 让定时同步、手动全量同步、补抓依次执行：同时跑会对同一批频道重复抓取，
	// 并且都认为「当天还没有节目」而各写一遍，造成节目重复
	syncMu sync.Mutex

	// 补抓请求合并执行：同一时间只有一个补抓任务，运行期间新请求的频道并入下一轮
	refillMu      sync.Mutex
	refillPending map[string]bool
	refillRunning bool
}

func NewSchedulerService(
	epgService *EPGService,
	channelService *ChannelService,
	channelMappingService *ChannelMappingService,
	chain *provider.Chain,
	cache cache.Cache,
) *SchedulerService {
	return &SchedulerService{
		cron:                  cron.New(),
		epgService:            epgService,
		channelService:        channelService,
		channelMappingService: channelMappingService,
		chain:                 chain,
		jobs:                  make(map[string]cron.EntryID),
		cache:                 cache,
	}
}

// SetCustomSources 接入自定义数据源。设置后，每次 SyncAllEPG 都会在所有 provider 之后同步它们，
// 所以自定义源始终最后写入、覆盖内置源
func (s *SchedulerService) SetCustomSources(customSources *CustomSourceService) {
	s.customSources = customSources
}

func (s *SchedulerService) Start() error {
	logger.Info("Starting scheduler service")

	if _, err := s.AddJob("sync_epg_midnight", "1 0 * * *", s.syncEPGMidnight); err != nil {
		return err
	}

	if _, err := s.AddJob("sync_epg_morning", "0 8 * * *", s.syncEPGMorning); err != nil {
		return err
	}

	if _, err := s.AddJob("cleanup_old_epg", "0 4 * * *", s.cleanupOldEPG); err != nil {
		return err
	}

	s.cron.Start()
	logger.Debug("Scheduler service started")

	return nil
}

func (s *SchedulerService) Stop() {
	logger.Info("Stopping scheduler service")
	ctx := s.cron.Stop()
	<-ctx.Done()
	logger.Info("Scheduler service stopped")
}

func (s *SchedulerService) AddJob(name, spec string, cmd func()) (cron.EntryID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[name]; exists {
		s.cron.Remove(entryID)
	}

	entryID, err := s.cron.AddFunc(spec, cmd)
	if err != nil {
		logger.Error("Failed to add job",
			logger.Err(err),
			logger.String("name", name),
			logger.String("spec", spec),
		)
		return 0, errors.Wrap(err, errors.ErrCodeUnknown, "failed to add job")
	}

	s.jobs[name] = entryID
	logger.Info("Added job",
		logger.String("name", name),
		logger.String("spec", spec),
	)

	return entryID, nil
}

func (s *SchedulerService) RemoveJob(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[name]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, name)
		logger.Info("Removed job", logger.String("name", name))
	}
}

func (s *SchedulerService) ListJobs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]string, 0, len(s.jobs))
	for name := range s.jobs {
		jobs = append(jobs, name)
	}

	return jobs
}

// SyncAllEPG 同步所有频道今天的节目单，最后同步自定义源。已有同步在进行时等它结束再开始
func (s *SchedulerService) SyncAllEPG(forceUpdate bool) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.syncLocked(context.Background(), forceUpdate, nil)
}

// RefillChannels 在后台补抓这些频道今天的内置节目（只补当天还没有节目的频道），然后重新同步自定义源。
// 自定义全量源的数据被清除后调用。补抓进行中再次调用时，新的频道并入下一轮，不会同时跑多个补抓
func (s *SchedulerService) RefillChannels(channelIDs []string) {
	if len(channelIDs) == 0 {
		return
	}
	s.refillMu.Lock()
	defer s.refillMu.Unlock()
	if s.refillPending == nil {
		s.refillPending = make(map[string]bool)
	}
	for _, id := range channelIDs {
		s.refillPending[id] = true
	}
	if s.refillRunning {
		return
	}
	s.refillRunning = true
	go s.refillLoop()
}

func (s *SchedulerService) refillLoop() {
	for {
		s.refillMu.Lock()
		if len(s.refillPending) == 0 {
			s.refillRunning = false
			s.refillMu.Unlock()
			return
		}
		only := s.refillPending
		s.refillPending = make(map[string]bool)
		s.refillMu.Unlock()

		s.syncMu.Lock()
		s.syncLocked(context.Background(), false, only)
		s.syncMu.Unlock()
	}
}

// syncLocked 是一轮同步：按优先级让各 provider 处理它负责的频道，最后同步自定义源。
// only 不为 nil 时只处理其中的频道。调用方负责持有 s.syncMu
func (s *SchedulerService) syncLocked(ctx context.Context, forceUpdate bool, only map[string]bool) {
	now := time.Now()
	startDate := now.AddDate(0, 0, 0)
	endDate := now.AddDate(0, 0, 0)

	syncType := "initial"
	if forceUpdate {
		syncType = "refresh"
	}
	var ids []string
	if only != nil {
		syncType = "refill"
		for id := range only {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	fields := []logger.Field{
		logger.String("sync_type", syncType),
		logger.Bool("force_update", forceUpdate),
	}
	if only != nil {
		fields = append(fields, logger.Strings("channels", ids))
	}
	logger.Info("Starting scheduled EPG sync", fields...)

	// 按优先级从高到低处理 provider（chain 已排好序）。
	// 强制刷新时记录本轮已刷新的「频道|日期」，低优先级的 provider 不再覆盖，见 RefreshEPGBatch
	providers := s.chain.GetProviders()
	replaced := make(map[string]bool)

	for _, p := range providers {
		channelMappings, err := s.channelMappingService.ListChannels(ctx, p.GetID())
		if err != nil {
			logger.Error("Failed to list channels",
				logger.Err(err),
				logger.String("provider_id", p.GetID()),
			)
			continue
		}
		var channelMappingInfos []*model.ChannelMappingInfo

		for _, cm := range channelMappings {
			if only != nil && !only[cm.CanonicalID] {
				continue
			}
			channelMappingInfos = append(channelMappingInfos, &model.ChannelMappingInfo{
				ProviderChannelID: cm.ProviderChannelID,
				CanonicalID:       cm.CanonicalID,
				ProviderID:        cm.ProviderID,
			})
		}

		if len(channelMappingInfos) == 0 {
			if only == nil {
				logger.Info("No channel mappings found for provider, skipping EPG sync",
					logger.String("provider_id", p.GetID()),
				)
			}
			continue
		}

		if forceUpdate {
			s.epgService.RefreshEPGBatch(ctx, p, channelMappingInfos, startDate, endDate, replaced)
			continue
		}

		if err := s.epgService.SyncEPGBatch(ctx, channelMappingInfos, startDate, endDate); err != nil {
			logger.Error("Failed to sync EPG batch",
				logger.Err(err),
				logger.String("provider_id", p.GetID()),
			)
		}
	}

	// 自定义源放在最后：同一频道同一时段以自定义源为准。
	// 它下载失败时不会动数据，频道自然回退到上面刚写入的内置源数据
	if s.customSources != nil {
		s.customSources.SyncAll(ctx)
	}

	logger.Info("Completed scheduled EPG sync", logger.String("sync_type", syncType))
}

func (s *SchedulerService) cleanupOldEPG() {
	ctx := context.Background()

	logger.Info("Starting scheduled EPG cleanup")

	count, err := s.epgService.CleanupOldEPG(ctx, 7)
	if err != nil {
		logger.Error("Failed to cleanup old EPG", logger.Err(err))
		return
	}

	logger.Info("Completed scheduled EPG cleanup", logger.Int64("deleted", count))
}

func (s *SchedulerService) RunNow(name string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.jobs[name]; !exists {
		return errors.NotFound("job", name)
	}

	switch name {
	case "sync_epg_midnight":
		go s.syncEPGMidnight()
	case "sync_epg_morning":
		go s.syncEPGMorning()
	case "cleanup_old_epg":
		go s.cleanupOldEPG()
	default:
		return errors.InvalidParam("name", "unknown job name")
	}

	logger.Info("Triggered job manually", logger.String("name", name))

	return nil
}

func (s *SchedulerService) syncEPGMidnight() {
	logger.Info("Running midnight EPG sync (initial sync only)")
	s.SyncAllEPG(false)
}

func (s *SchedulerService) syncEPGMorning() {
	logger.Info("Running morning EPG sync (force refresh)")
	s.SyncAllEPG(true)
}
