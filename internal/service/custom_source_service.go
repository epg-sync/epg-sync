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
// 导入以「时间段」为单位覆盖：文件里节目连续覆盖到的时间段以文件为准（不论原来是哪个 provider 写的），
// 文件没覆盖到的时间段（包括节目之间的空档）保留原有节目。
// 调度时它在所有 provider 之后执行，所以始终优先于内置源。
//
// 多个源之间靠写入顺序体现优先级：优先级低的先写、高的后写。所以任何一次同步都必须按优先级
// 跑完所有已启用的源，单独同步某一个源会让它盖掉优先级更高的源
type CustomSourceService struct {
	repo        repository.CustomSourceRepository
	channelRepo repository.ChannelRepository
	programRepo repository.ProgramRepository
	cache       cache.Cache

	mu sync.Mutex // 同一时间只允许一个任务写库，避免定时任务、手动同步、停用、删除相互覆盖
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

// Create 保存新的数据源；已启用时在后台按优先级同步一次全部已启用的源
func (s *CustomSourceService) Create(ctx context.Context, src *model.CustomSource) (*model.CustomSource, error) {
	if err := validateCustomSource(src); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, src); err != nil {
		return nil, err
	}
	if src.Enabled == 1 {
		s.startAllQuietly()
	}
	return src, nil
}

// Update 更新 name / url / enabled / priority。
// 停用时清除它写入的节目，再重新同步其余的源，让被它覆盖掉的低优先级源的数据回来；
// 新启用或改了地址时在后台重新同步
func (s *CustomSourceService) Update(ctx context.Context, src *model.CustomSource) (*model.CustomSource, error) {
	if err := validateCustomSource(src); err != nil {
		return nil, err
	}
	old, err := s.repo.GetByID(ctx, src.ID)
	if err != nil {
		return nil, err
	}

	if old.Enabled == 1 && src.Enabled != 1 {
		if !s.mu.TryLock() {
			return nil, ErrSyncInProgress
		}
		// 先清节目再改状态：清除失败时源仍是启用状态，可以重试
		err := s.purgeLocked(ctx, old)
		if err == nil {
			err = s.repo.Update(ctx, src)
		}
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		s.startAllQuietly()
		return s.repo.GetByID(ctx, src.ID)
	}

	if err := s.repo.Update(ctx, src); err != nil {
		return nil, err
	}
	if src.Enabled == 1 && (old.Enabled != 1 || old.URL != src.URL) {
		s.startAllQuietly()
	}
	return s.repo.GetByID(ctx, src.ID)
}

// Delete 删除数据源并清理它写入的节目，然后重新同步其余的源（被覆盖掉的内置节目要等下一次全量同步才会补回来）。
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
	deleted, err := s.programRepo.DeleteByProviderID(ctx, src.ProviderID())
	if err == nil {
		err = s.repo.Delete(ctx, id)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}

	s.invalidateXMLTVCache(ctx)
	logger.Info("Deleted custom source",
		logger.Int64("id", id),
		logger.String("name", src.Name),
		logger.Int64("programs_deleted", deleted),
	)
	s.startAllQuietly()
	return nil
}

// purgeLocked 清除一个源写入的全部节目和同步状态。调用方负责持有 s.mu
func (s *CustomSourceService) purgeLocked(ctx context.Context, src *model.CustomSource) error {
	deleted, err := s.programRepo.DeleteByProviderID(ctx, src.ProviderID())
	if err != nil {
		return err
	}
	if err := s.repo.ClearSyncResult(ctx, src.ID); err != nil {
		return err
	}
	s.invalidateXMLTVCache(ctx)
	logger.Info("Purged programs of disabled custom source",
		logger.Int64("id", src.ID),
		logger.String("name", src.Name),
		logger.Int64("programs_deleted", deleted),
	)
	return nil
}

// StartSync 是「立即同步」按钮：在后台按优先级同步全部已启用的源并立即返回。
// 不能只同步 id 这一个，否则它会盖掉优先级更高的源。id 对应的源必须是启用状态；
// 已有同步在进行时返回 ErrSyncInProgress
func (s *CustomSourceService) StartSync(ctx context.Context, id int64) error {
	src, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if src.Enabled != 1 {
		return errors.InvalidParam("enabled", "数据源已停用，启用后才能同步")
	}
	return s.startAll(ctx)
}

// startAll 在后台按优先级同步全部已启用的源。返回前已把它们标记为 running
func (s *CustomSourceService) startAll(ctx context.Context) error {
	if !s.mu.TryLock() {
		return ErrSyncInProgress
	}
	sources, err := s.enabledSources(ctx)
	if err != nil || len(sources) == 0 {
		s.mu.Unlock()
		return err
	}
	s.markRunning(ctx, sources)
	go func() {
		defer s.mu.Unlock()
		s.runLocked(context.Background(), sources)
	}()
	return nil
}

func (s *CustomSourceService) startAllQuietly() {
	if err := s.startAll(context.Background()); err != nil {
		logger.Warn("Skipped immediate custom source sync, it will run with the next scheduled sync",
			logger.Err(err))
	}
}

// SyncAll 按优先级同步所有已启用的数据源，供定时任务调用；已有同步在进行时直接跳过。
// 单个源失败只记在它自己的状态上，不影响其它源
func (s *CustomSourceService) SyncAll(ctx context.Context) {
	if !s.mu.TryLock() {
		logger.Warn("Custom source sync already in progress, skipping")
		return
	}
	defer s.mu.Unlock()

	sources, err := s.enabledSources(ctx)
	if err != nil {
		logger.Error("Failed to list custom sources", logger.Err(err))
		return
	}
	s.markRunning(ctx, sources)
	s.runLocked(ctx, sources)
}

// enabledSources 返回已启用的源，按 priority、id 升序
func (s *CustomSourceService) enabledSources(ctx context.Context) ([]*model.CustomSource, error) {
	all, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	enabled := make([]*model.CustomSource, 0, len(all))
	for _, src := range all {
		if src.Enabled == 1 {
			enabled = append(enabled, src)
		}
	}
	return enabled, nil
}

func (s *CustomSourceService) markRunning(ctx context.Context, sources []*model.CustomSource) {
	for _, src := range sources {
		if err := s.repo.SetRunning(ctx, src.ID); err != nil {
			logger.Error("Failed to mark custom source running", logger.Err(err), logger.Int64("id", src.ID))
		}
	}
}

// runLocked 倒序处理按 priority 升序排好的源：数字大的先写、数字小的后写，让优先级高的最终覆盖。
// 调用方负责持有 s.mu
func (s *CustomSourceService) runLocked(ctx context.Context, sources []*model.CustomSource) {
	for i := len(sources) - 1; i >= 0; i-- {
		s.syncOne(ctx, sources[i])
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
		if err := s.programRepo.ReplaceChannelRanges(ctx, id, coveredRanges(programs), src.ProviderID(), win.From, programs); err != nil {
			logger.Error("Failed to write custom source programs",
				logger.Err(err), logger.Int64("source_id", src.ID), logger.String("channel_id", id))
			failed = append(failed, id)
			continue
		}
		total += len(programs)
	}

	// 文件里已经没有的频道（或改了正则后对应到别的频道），上次写入的节目要清掉。
	// 写入失败的频道也在 ids 里，保留它们上次的数据
	stale, err := s.programRepo.DeleteByProviderOutsideChannels(ctx, src.ProviderID(), ids, win.From)
	if err != nil {
		return total, err
	}
	if stale > 0 {
		logger.Info("Removed programs of channels no longer in custom source",
			logger.Int64("source_id", src.ID), logger.Int64("deleted", stale))
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

// coveredRanges 把按开始时间排好序的节目合并成连续的时间段：首尾相接或重叠的并成一段，
// 中间有空档的分成两段。只有这些时间段里的原有节目会被覆盖，空档里的保留
func coveredRanges(programs []*model.Program) []model.TimeRange {
	var ranges []model.TimeRange
	for _, p := range programs {
		if n := len(ranges); n > 0 && !p.StartTime.After(ranges[n-1].To) {
			if p.EndTime.After(ranges[n-1].To) {
				ranges[n-1].To = p.EndTime
			}
			continue
		}
		ranges = append(ranges, model.TimeRange{From: p.StartTime, To: p.EndTime})
	}
	return ranges
}

// 名字末尾的清晰度标记（含前面的空格、横线）。不含 4K：4K 频道通常是独立的节目单
var qualitySuffix = regexp.MustCompile(`(?i)[\s_\-]*(超高清|高清|标清|uhd|hd|sd)$`)

// stripQuality 反复去掉末尾的清晰度标记，可能返回空串
func stripQuality(name string) string {
	name = strings.TrimSpace(name)
	for {
		trimmed := strings.TrimSpace(qualitySuffix.ReplaceAllString(name, ""))
		if trimmed == name {
			return name
		}
		name = trimmed
	}
}

// normalizeChannelName 去掉首尾空白和末尾的清晰度标记，如 "CCTV-1 高清" -> "CCTV-1"；
// 名字全是清晰度标记时原样返回
func normalizeChannelName(name string) string {
	if n := stripQuality(name); n != "" {
		return n
	}
	return strings.TrimSpace(name)
}

// nameRegexp 生成新频道的匹配正则，让 /api/diyp 这类按名称查询的接口能找到它（含清晰度后缀的写法）
func nameRegexp(name string) string {
	return `^` + regexp.QuoteMeta(name) + `(\s*(超高清|高清|超清|标清|hd|uhd|sd))?$`
}

const nameSeparators = " \t-_·|"

// regexpRemainderOK 判断正则匹配后剩下的部分是否仍指同一个频道。
//
// 种子频道的正则大多只锚定开头（如 ^广东卫视），会把「广东卫视国际」也匹配进来。
// 剩下的部分为空、只是清晰度标记或「频道」，或者是频道名称里本来就有的文字（如 CCTV-5+ 后面的「体育赛事」），
// 才认为是同一个频道
func regexpRemainderOK(remainder string, ch *model.Channel) bool {
	rest := strings.Trim(remainder, nameSeparators)
	rest = strings.Trim(stripQuality(rest), nameSeparators)
	rest = strings.Trim(strings.TrimSuffix(rest, "频道"), nameSeparators)
	if rest == "" {
		return true
	}
	rest = strings.ToLower(rest)
	return strings.Contains(strings.ToLower(ch.DisplayName), rest) ||
		strings.Contains(strings.ToLower(ch.ChannelID), rest)
}

// channelResolver 把文件里的频道对应到数据库里的频道，对应不上就新建
type channelResolver struct {
	repo    repository.ChannelRepository
	byID    map[string]*model.Channel // 小写 channel_id
	byName  map[string]*model.Channel // 小写 display_name
	regexps map[string]*model.Channel // 正则匹配结果（含匹配不到的 nil），避免重复查库
}

func newChannelResolver(ctx context.Context, repo repository.ChannelRepository) (*channelResolver, error) {
	all, err := repo.GetAllChannels(ctx)
	if err != nil {
		return nil, err
	}
	r := &channelResolver{
		repo:    repo,
		byID:    make(map[string]*model.Channel, len(all)),
		byName:  make(map[string]*model.Channel, len(all)),
		regexps: make(map[string]*model.Channel),
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

// resolve 依次按 channel_id 精确匹配、display_name 精确匹配、名称正则查找，都没有就新建。
// 精确匹配排在正则前面，避免宽松的前缀正则抢先匹配到别的频道。
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

	// 2. display_name 精确匹配
	for _, c := range candidates {
		if ch := r.byName[strings.ToLower(c)]; ch != nil {
			return ch, nil
		}
	}

	// 3. 名称正则。种子频道的正则用了 Postgres 才支持的前瞻语法，只能让数据库来匹配
	for _, c := range candidates {
		ch, err := r.matchByRegexp(ctx, c)
		if err != nil {
			return nil, err
		}
		if ch != nil {
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

// matchByRegexp 返回正则能匹配 name、且剩下的部分仍指同一频道的第一个频道（按 id），没有则返回 nil
func (r *channelResolver) matchByRegexp(ctx context.Context, name string) (*model.Channel, error) {
	if ch, ok := r.regexps[name]; ok {
		return ch, nil
	}
	matches, err := r.repo.MatchByRegexp(ctx, name)
	if err != nil {
		return nil, err
	}
	var found *model.Channel
	for _, m := range matches {
		if regexpRemainderOK(strings.Replace(name, m.Matched, "", 1), &m.Channel) {
			found = &m.Channel
			break
		}
	}
	r.regexps[name] = found
	return found, nil
}
