package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/internal/xmltv"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

const (
	// 只导入这个窗口内的节目：往前 48 小时（XMLTV 输出含昨天）、往后 7 天（和 7 天的清理周期一致）
	customSourcePast   = 48 * time.Hour
	customSourceFuture = 7 * 24 * time.Hour

	defaultChannelTimezone = "Asia/Shanghai"
)

// ErrSyncInProgress 表示已有自定义源在同步，同一时间只允许一个同步任务写库
var ErrSyncInProgress = errors.New(errors.ErrCodeAlreadyExists, "custom source sync already in progress")

// CustomSourceService 管理用户配置的 XMLTV 文件源，并把文件里的节目导入数据库。
//
// 导入以「时间范围」为单位覆盖：文件覆盖到的时间段以文件为准（不论原来是哪个 provider 写的），
// 文件没覆盖到的时间段保留原有节目。调度时它在所有 provider 之后执行，所以始终优先于内置源
type CustomSourceService struct {
	repo        repository.CustomSourceRepository
	channelRepo repository.ChannelRepository
	programRepo repository.ProgramRepository
	cache       cache.Cache

	mu sync.Mutex // 同一时间只允许一个同步任务，避免定时任务和手动触发同时写同一批频道
}

func NewCustomSourceService(
	repo repository.CustomSourceRepository,
	channelRepo repository.ChannelRepository,
	programRepo repository.ProgramRepository,
	cache cache.Cache,
) *CustomSourceService {
	return &CustomSourceService{repo: repo, channelRepo: channelRepo, programRepo: programRepo, cache: cache}
}

// ResetStale 把进程中途退出留下的 running 状态改为 failed，启动时调用
func (s *CustomSourceService) ResetStale(ctx context.Context) error {
	return s.repo.ResetRunning(ctx)
}

func (s *CustomSourceService) List(ctx context.Context) ([]*model.CustomSource, error) {
	return s.repo.List(ctx)
}

func (s *CustomSourceService) Get(ctx context.Context, id int64) (*model.CustomSource, error) {
	return s.repo.GetByID(ctx, id)
}

func validateCustomSource(src *model.CustomSource) error {
	src.Name = strings.TrimSpace(src.Name)
	src.URL = strings.TrimSpace(src.URL)
	if src.Name == "" {
		return errors.InvalidParam("name", "name is required")
	}
	if err := xmltv.ValidateURL(src.URL); err != nil {
		return errors.InvalidParam("url", err.Error())
	}
	return nil
}

// Create 保存新的数据源；已启用时在后台立即同步一次
func (s *CustomSourceService) Create(ctx context.Context, src *model.CustomSource) (*model.CustomSource, error) {
	if err := validateCustomSource(src); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, src); err != nil {
		return nil, err
	}
	if src.Enabled == 1 {
		s.startSyncQuietly(src.ID)
	}
	return src, nil
}

// Update 更新 name / url / enabled / priority；新启用或改了地址时在后台立即同步一次
func (s *CustomSourceService) Update(ctx context.Context, src *model.CustomSource) (*model.CustomSource, error) {
	if err := validateCustomSource(src); err != nil {
		return nil, err
	}
	old, err := s.repo.GetByID(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, src); err != nil {
		return nil, err
	}
	if src.Enabled == 1 && (old.Enabled != 1 || old.URL != src.URL) {
		s.startSyncQuietly(src.ID)
	}
	return s.repo.GetByID(ctx, src.ID)
}

// Delete 删除数据源并清理它写入的节目（被覆盖掉的内置节目要等下一次同步才会补回来）。
// 自动创建的频道保留，可以在频道管理页手动删除
func (s *CustomSourceService) Delete(ctx context.Context, id int64) error {
	src, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	// 同步进行中删除，同步结束时可能把刚清掉的节目又写回来
	if !s.mu.TryLock() {
		return ErrSyncInProgress
	}
	defer s.mu.Unlock()

	deleted, err := s.programRepo.DeleteByProviderID(ctx, src.ProviderID())
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidateXMLTVCache(ctx)
	logger.Info("Deleted custom source",
		logger.Int64("id", id),
		logger.String("name", src.Name),
		logger.Int64("programs_deleted", deleted),
	)
	return nil
}

// StartSync 在后台同步一个数据源（不论是否启用），立即返回。已有同步在进行时返回 ErrSyncInProgress
func (s *CustomSourceService) StartSync(ctx context.Context, id int64) error {
	src, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if !s.mu.TryLock() {
		return ErrSyncInProgress
	}
	if err := s.repo.SetRunning(ctx, id); err != nil {
		s.mu.Unlock()
		return err
	}
	go func() {
		defer s.mu.Unlock()
		s.syncOne(context.Background(), src)
	}()
	return nil
}

func (s *CustomSourceService) startSyncQuietly(id int64) {
	if err := s.StartSync(context.Background(), id); err != nil {
		logger.Warn("Skipped immediate custom source sync, use the sync button later",
			logger.Int64("id", id), logger.Err(err))
	}
}

// SyncAll 依次同步所有已启用的数据源。优先级数字大的先写、数字小的后写，让优先级高的最终覆盖；
// 已有同步在进行时直接跳过。单个源失败只记在它自己的状态上，不影响其它源
func (s *CustomSourceService) SyncAll(ctx context.Context) {
	if !s.mu.TryLock() {
		logger.Warn("Custom source sync already in progress, skipping")
		return
	}
	defer s.mu.Unlock()

	sources, err := s.repo.List(ctx)
	if err != nil {
		logger.Error("Failed to list custom sources", logger.Err(err))
		return
	}

	// List 按 priority、id 升序，倒着处理
	for i := len(sources) - 1; i >= 0; i-- {
		src := sources[i]
		if src.Enabled != 1 {
			continue
		}
		if err := s.repo.SetRunning(ctx, src.ID); err != nil {
			logger.Error("Failed to mark custom source running", logger.Err(err), logger.Int64("id", src.ID))
			continue
		}
		s.syncOne(ctx, src)
	}
}

// syncOne 导入一个源并把结果写回它的状态。调用方负责持有 s.mu
func (s *CustomSourceService) syncOne(ctx context.Context, src *model.CustomSource) {
	started := time.Now()
	count, err := s.importSource(ctx, src)

	status, msg := model.CustomSourceStatusSuccess, ""
	if err != nil {
		status, msg = model.CustomSourceStatusFailed, err.Error()
		logger.Error("Custom source sync failed",
			logger.Int64("id", src.ID), logger.String("name", src.Name), logger.Err(err))
		if count == 0 {
			count = src.ProgramCount // 完全失败时保留上次的节目数
		}
	} else {
		logger.Info("Custom source synced",
			logger.Int64("id", src.ID), logger.String("name", src.Name),
			logger.Int("programs", count), logger.Duration("took", time.Since(started)))
	}

	if err := s.repo.SetSyncResult(ctx, src.ID, status, msg, count); err != nil {
		logger.Error("Failed to save custom source sync result", logger.Err(err), logger.Int64("id", src.ID))
	}
	s.invalidateXMLTVCache(ctx)
}

func (s *CustomSourceService) invalidateXMLTVCache(ctx context.Context) {
	s.cache.Delete(ctx, xmltvCacheKey(time.Now()))
}

// importSource 下载、解析并写库，返回写入的节目数。文件里没有可用节目时返回错误且不动现有数据
func (s *CustomSourceService) importSource(ctx context.Context, src *model.CustomSource) (int, error) {
	now := time.Now()
	win := xmltv.Window{From: now.Add(-customSourcePast), To: now.Add(customSourceFuture)}

	rc, err := xmltv.Fetch(ctx, src.URL, provider.DefaultUserAgent)
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	res, err := xmltv.Parse(rc, win)
	if err != nil {
		return 0, err
	}
	if len(res.Programmes) == 0 {
		return 0, fmt.Errorf("文件中没有可用节目（共 %d 条，其中时间无效或缺标题 %d 条，已过期或太远 %d 条），可能是文件已过期",
			res.Total, res.BadTime, res.OutOfWindow)
	}

	resolver, err := newChannelResolver(ctx, s.channelRepo)
	if err != nil {
		return 0, err
	}

	fileChannels := make(map[string]*xmltv.Channel, len(res.Channels))
	for _, c := range res.Channels {
		fileChannels[c.ID] = c
	}

	resolved := make(map[string]*model.Channel) // 文件里的频道 id -> 数据库频道
	channels := make(map[string]*model.Channel) // 数据库频道 id -> 频道
	grouped := make(map[string][]*xmltv.Programme)
	for _, p := range res.Programmes {
		ch, ok := resolved[p.ChannelID]
		if !ok {
			info := fileChannels[p.ChannelID]
			if info == nil {
				info = &xmltv.Channel{ID: p.ChannelID}
			}
			if ch, err = resolver.resolve(ctx, info); err != nil {
				return 0, err
			}
			resolved[p.ChannelID] = ch
		}
		channels[ch.ChannelID] = ch
		grouped[ch.ChannelID] = append(grouped[ch.ChannelID], p)
	}

	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	total := 0
	var failed []string
	for _, id := range ids {
		programs := buildPrograms(src, channels[id], grouped[id])
		from, to := programs[0].StartTime, programs[0].EndTime
		for _, p := range programs {
			if p.EndTime.After(to) {
				to = p.EndTime
			}
		}
		if err := s.programRepo.ReplaceChannelRange(ctx, id, from, to, src.ProviderID(), win.From, programs); err != nil {
			logger.Error("Failed to write custom source programs",
				logger.Err(err), logger.Int64("source_id", src.ID), logger.String("channel_id", id))
			failed = append(failed, id)
			continue
		}
		total += len(programs)
	}

	if len(failed) > 0 {
		shown := failed
		if len(shown) > 5 {
			shown = shown[:5]
		}
		return total, fmt.Errorf("%d 个频道写入失败：%s", len(failed), strings.Join(shown, "、"))
	}
	return total, nil
}

// buildPrograms 把同一个频道的节目转成入库格式：按开始时间排序，开始时间相同的只留第一条
func buildPrograms(src *model.CustomSource, ch *model.Channel, in []*xmltv.Programme) []*model.Program {
	sorted := append([]*xmltv.Programme(nil), in...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })

	tz := ch.Timezone
	if tz == "" {
		tz = defaultChannelTimezone
	}

	out := make([]*model.Program, 0, len(sorted))
	for _, p := range sorted {
		if n := len(out); n > 0 && out[n-1].StartTime.Equal(p.Start) {
			continue
		}
		out = append(out, &model.Program{
			ChannelID:        ch.ChannelID,
			Title:            p.Title,
			Description:      p.Description,
			Category:         p.Category,
			StartTime:        p.Start,
			EndTime:          p.Stop,
			ProviderID:       src.ProviderID(),
			OriginalTimezone: tz,
		})
	}
	return out
}

// 去掉频道名末尾的清晰度标记（含前面的空格、横线）。不含 4K：4K 频道通常是独立的节目单
var qualitySuffix = regexp.MustCompile(`(?i)[\s_\-]*(超高清|高清|标清|uhd|hd|sd)$`)

// normalizeChannelName 去掉首尾空白和末尾的清晰度标记，如 "CCTV-1 高清" -> "CCTV-1"
func normalizeChannelName(name string) string {
	name = strings.TrimSpace(name)
	for {
		trimmed := strings.TrimSpace(qualitySuffix.ReplaceAllString(name, ""))
		if trimmed == "" || trimmed == name {
			return name
		}
		name = trimmed
	}
}

// nameRegexp 生成新频道的匹配正则，让 /api/diyp 这类按名称查询的接口能找到它（含清晰度后缀的写法）
func nameRegexp(name string) string {
	return `^` + regexp.QuoteMeta(name) + `(\s*(超高清|高清|超清|标清|hd|uhd|sd))?$`
}

// channelResolver 把文件里的频道对应到数据库里的频道，对应不上就新建
type channelResolver struct {
	repo   repository.ChannelRepository
	byID   map[string]*model.Channel // 小写 channel_id
	byName map[string]*model.Channel // 小写 display_name
	misses map[string]bool           // 正则匹配不到的名字，避免重复查库
	hits   map[string]*model.Channel // 正则匹配到的名字
}

func newChannelResolver(ctx context.Context, repo repository.ChannelRepository) (*channelResolver, error) {
	all, err := repo.GetAllChannels(ctx)
	if err != nil {
		return nil, err
	}
	r := &channelResolver{
		repo:   repo,
		byID:   make(map[string]*model.Channel, len(all)),
		byName: make(map[string]*model.Channel, len(all)),
		misses: make(map[string]bool),
		hits:   make(map[string]*model.Channel),
	}
	for _, ch := range all {
		r.index(ch)
	}
	return r, nil
}

func (r *channelResolver) index(ch *model.Channel) {
	r.byID[strings.ToLower(ch.ChannelID)] = ch
	if ch.DisplayName != "" {
		if _, exists := r.byName[strings.ToLower(ch.DisplayName)]; !exists {
			r.byName[strings.ToLower(ch.DisplayName)] = ch
		}
	}
}

// resolve 依次按 channel_id 精确匹配、名称正则、display_name 精确匹配查找，都没有就新建。
// 已存在的频道（内置的、用户手动改过的）不会被修改
func (r *channelResolver) resolve(ctx context.Context, info *xmltv.Channel) (*model.Channel, error) {
	names := info.DisplayNames
	if len(names) == 0 {
		names = []string{info.ID}
	}

	// 每个名字依次尝试原名和去掉清晰度后缀的名字
	var candidates []string
	for _, name := range names {
		candidates = append(candidates, name)
		if n := normalizeChannelName(name); n != name {
			candidates = append(candidates, n)
		}
	}

	// 1. channel_id 精确匹配（文件里的频道 id 和各个名字都试）
	for _, c := range append([]string{info.ID}, candidates...) {
		if ch := r.byID[strings.ToLower(c)]; ch != nil {
			return ch, nil
		}
	}

	// 2. 名称正则。种子频道的正则用了 Postgres 才支持的前瞻语法，只能让数据库来匹配
	for _, c := range candidates {
		ch, err := r.matchByRegexp(ctx, c)
		if err != nil {
			return nil, err
		}
		if ch != nil {
			return ch, nil
		}
	}

	// 3. display_name 精确匹配
	for _, c := range candidates {
		if ch := r.byName[strings.ToLower(c)]; ch != nil {
			return ch, nil
		}
	}

	// 4. 新建：用去掉清晰度后缀的名字作 channel_id，多个源、多次同步都落在同一个频道上
	id := normalizeChannelName(names[0])
	if id == "" {
		id = info.ID
	}
	ch := &model.Channel{
		ChannelID:   id,
		DisplayName: names[0],
		Area:        "CN",
		LogoURL:     info.IconURL,
		Regexp:      nameRegexp(id),
		Timezone:    defaultChannelTimezone,
		IsActive:    1,
	}
	if err := r.repo.Create(ctx, ch); err != nil {
		return nil, err
	}
	logger.Info("Created channel from custom source",
		logger.String("channel_id", ch.ChannelID), logger.String("display_name", ch.DisplayName))
	r.index(ch)
	return ch, nil
}

func (r *channelResolver) matchByRegexp(ctx context.Context, name string) (*model.Channel, error) {
	if ch, ok := r.hits[name]; ok {
		return ch, nil
	}
	if r.misses[name] {
		return nil, nil
	}
	ch, err := r.repo.GetByChannelName(ctx, name)
	if err != nil {
		if errors.Is(err, errors.ErrCodeChannelNotFound) {
			r.misses[name] = true
			return nil, nil
		}
		return nil, err
	}
	r.hits[name] = ch
	return ch, nil
}
