package service

import (
	"context"
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
	mu                    sync.RWMutex
	jobs                  map[string]cron.EntryID
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

func (s *SchedulerService) SyncAllEPG(forceUpdate bool) {
	ctx := context.Background()
	now := time.Now()
	startDate := now.AddDate(0, 0, 0)
	endDate := now.AddDate(0, 0, 0)

	syncType := "initial"
	if forceUpdate {
		syncType = "refresh"
	}
	logger.Info("Starting scheduled EPG sync",
		logger.String("sync_type", syncType),
		logger.Bool("force_update", forceUpdate),
	)

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
			channelMappingInfos = append(channelMappingInfos, &model.ChannelMappingInfo{
				ProviderChannelID: cm.ProviderChannelID,
				CanonicalID:       cm.CanonicalID,
				ProviderID:        cm.ProviderID,
			})
		}

		if len(channelMappingInfos) == 0 {
			logger.Info("No channel mappings found for provider, skipping EPG sync",
				logger.String("provider_id", p.GetID()),
			)
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

	logger.Info("Completed scheduled EPG sync")
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
