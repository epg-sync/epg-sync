package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/database/dbtest"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/provider"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/internal/repository/postgres"
	"gorm.io/gorm"
)

// fakeProvider 按频道返回预设的节目或错误，并记录被请求过的频道
type fakeProvider struct {
	*provider.BaseProvider
	results map[string]fakeResult // key 为 canonical channel id

	mu        sync.Mutex
	requested []string
}

type fakeResult struct {
	titles []string
	err    error
}

func newFakeProvider(id string, priority int, results map[string]fakeResult) *fakeProvider {
	return &fakeProvider{
		BaseProvider: provider.NewBaseProvider(&model.ProviderConfig{ID: id, Priority: priority, Enabled: true}, nil),
		results:      results,
	}
}

func (f *fakeProvider) FetchEPG(_ context.Context, _, channelID string, date time.Time) ([]*model.Program, error) {
	f.mu.Lock()
	f.requested = append(f.requested, channelID)
	f.mu.Unlock()

	r := f.results[channelID]
	if r.err != nil {
		return nil, r.err
	}
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	programs := make([]*model.Program, len(r.titles))
	for i, title := range r.titles {
		programs[i] = &model.Program{
			ChannelID:  channelID,
			Title:      title,
			StartTime:  day.Add(time.Duration(i) * time.Hour),
			EndTime:    day.Add(time.Duration(i+1) * time.Hour),
			ProviderID: f.GetID(),
		}
	}
	return programs, nil
}

func (f *fakeProvider) FetchEPGBatch(ctx context.Context, infos []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return f.BaseProvider.FetchEPGBatch(ctx, f, infos, date)
}

func (f *fakeProvider) HealthCheck(context.Context) *model.ProviderHealth {
	return &model.ProviderHealth{Healthy: true}
}

func (f *fakeProvider) requestedChannels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.requested...)
	sort.Strings(out)
	return out
}

type refreshEnv struct {
	db        *gorm.DB
	programs  repository.ProgramRepository
	scheduler *SchedulerService
}

// newRefreshEnv 用真实的 Postgres 仓库和给定的 provider 搭起 SchedulerService。
// mappings 的 key 为 provider id，值为该 provider 负责的 canonical 频道
func newRefreshEnv(t *testing.T, mappings map[string][]string, providers ...provider.Provider) *refreshEnv {
	t.Helper()
	db := dbtest.New(t)
	ctx := context.Background()

	channelRepo := postgres.NewChannelRepository(db)
	mappingRepo := postgres.NewChannelMappingsRepository(db)
	programRepo := postgres.NewProgramRepository(db)
	c := cache.NewMemoryCache()

	for providerID, channels := range mappings {
		for _, ch := range channels {
			if err := mappingRepo.Create(ctx, &model.ChannelMapping{
				CanonicalID: ch, ProviderID: providerID, ProviderChannelID: providerID + "-" + ch, Confidence: 1,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	chain := provider.NewChain(c, providers...)
	epg := NewEPGService(programRepo, channelRepo, mappingRepo, c, chain)
	channelService := NewChannelService(channelRepo, mappingRepo, c, chain)
	mappingService := NewChannelMappingService(mappingRepo, channelRepo)
	return &refreshEnv{
		db:        db,
		programs:  programRepo,
		scheduler: NewSchedulerService(epg, channelService, mappingService, chain, c),
	}
}

// seed 写入某频道今天的旧节目，模拟之前同步留下的数据
func (e *refreshEnv) seed(t *testing.T, channelID, providerID string, titles ...string) {
	t.Helper()
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var programs []*model.Program
	for i, title := range titles {
		programs = append(programs, &model.Program{
			ChannelID: channelID, Title: title, ProviderID: providerID,
			StartTime: day.Add(time.Duration(i) * time.Hour), EndTime: day.Add(time.Duration(i+1) * time.Hour),
		})
	}
	if err := e.programs.CreateBatch(context.Background(), programs); err != nil {
		t.Fatal(err)
	}
}

// today 返回频道今天的节目，格式为 "provider:title"，按开始时间排序
func (e *refreshEnv) today(t *testing.T, channelID string) []string {
	t.Helper()
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var programs []*model.Program
	if err := e.db.Where("channel_id = ? AND start_time >= ? AND start_time < ?", channelID, day, day.AddDate(0, 0, 1)).
		Order("start_time").Find(&programs).Error; err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(programs))
	for i, p := range programs {
		out[i] = p.ProviderID + ":" + p.Title
	}
	return out
}

func assertPrograms(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// 纬来体育的场景：已有低优先级 homeplus 的数据，强制刷新后应换成高优先级 vl 的数据，
// homeplus 不再被请求这个频道
func TestForceRefreshPrefersHigherPriority(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{"纬来体育": {titles: []string{"vl-a", "vl-b"}}})
	homeplus := newFakeProvider("homeplus", 5, map[string]fakeResult{"纬来体育": {titles: []string{"hp-new"}}})
	env := newRefreshEnv(t, map[string][]string{"vl": {"纬来体育"}, "homeplus": {"纬来体育"}}, vl, homeplus)
	env.seed(t, "纬来体育", "homeplus", "hp-old-1", "hp-old-2", "hp-old-3")

	env.scheduler.SyncAllEPG(true)

	assertPrograms(t, env.today(t, "纬来体育"), "vl:vl-a", "vl:vl-b")
	if got := homeplus.requestedChannels(); len(got) != 0 {
		t.Errorf("homeplus was asked for %v after vl already refreshed the channel", got)
	}
}

// 高优先级抓取失败时，由下一个 provider 刷新
func TestForceRefreshFallsBackWhenHigherPriorityFails(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{"纬来体育": {err: errors.New("site down")}})
	homeplus := newFakeProvider("homeplus", 5, map[string]fakeResult{"纬来体育": {titles: []string{"hp-new"}}})
	env := newRefreshEnv(t, map[string][]string{"vl": {"纬来体育"}, "homeplus": {"纬来体育"}}, vl, homeplus)
	env.seed(t, "纬来体育", "homeplus", "hp-old")

	env.scheduler.SyncAllEPG(true)

	assertPrograms(t, env.today(t, "纬来体育"), "homeplus:hp-new")
}

// 所有 provider 都失败或没有数据时保留旧数据（之前会先删掉，导致当天节目单清空）
func TestForceRefreshKeepsDataWhenAllFail(t *testing.T) {
	iqilu := newFakeProvider("iqilu", 1, map[string]fakeResult{"山东农科": {titles: nil}})
	env := newRefreshEnv(t, map[string][]string{"iqilu": {"山东农科"}}, iqilu)
	env.seed(t, "山东农科", "iqilu", "old-1", "old-2")

	env.scheduler.SyncAllEPG(true)

	assertPrograms(t, env.today(t, "山东农科"), "iqilu:old-1", "iqilu:old-2")
}

// 同一批中一个频道失败，不影响其他频道刷新
func TestForceRefreshPartialBatchFailure(t *testing.T) {
	iqilu := newFakeProvider("iqilu", 1, map[string]fakeResult{
		"山东卫视": {titles: []string{"new"}},
		"山东农科": {err: errors.New("bad time format")},
	})
	env := newRefreshEnv(t, map[string][]string{"iqilu": {"山东卫视", "山东农科"}}, iqilu)
	env.seed(t, "山东卫视", "iqilu", "old")
	env.seed(t, "山东农科", "iqilu", "old")

	env.scheduler.SyncAllEPG(true)

	assertPrograms(t, env.today(t, "山东卫视"), "iqilu:new")
	assertPrograms(t, env.today(t, "山东农科"), "iqilu:old")
}

// 非强制同步保持原有行为：频道已有节目（不论来自哪个 provider）就跳过
func TestSyncSkipsExistingWithoutForce(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{
		"纬来体育": {titles: []string{"vl-a"}},
		"山东卫视": {titles: []string{"vl-x"}},
	})
	env := newRefreshEnv(t, map[string][]string{"vl": {"纬来体育", "山东卫视"}}, vl)
	env.seed(t, "纬来体育", "homeplus", "hp-old")

	env.scheduler.SyncAllEPG(false)

	assertPrograms(t, env.today(t, "纬来体育"), "homeplus:hp-old")
	assertPrograms(t, env.today(t, "山东卫视"), "vl:vl-x")
}

// 全量自定义源在所有 provider 之后执行，当天整天以它为准；它下载失败时频道回退到 provider 的数据
func TestSyncAllEPGAppliesCustomSourcesLast(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{"CCTV1": {titles: []string{"vl-a", "vl-b"}}})
	env := newRefreshEnv(t, map[string][]string{"vl": {"CCTV1"}}, vl)

	customRepo := postgres.NewCustomSourceRepository(env.db)
	custom := NewCustomSourceService(customRepo, postgres.NewChannelRepository(env.db), env.programs, cache.NewMemoryCache())
	env.scheduler.SetCustomSources(custom)

	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	files := newFileServer(t)
	files.set("/epg.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"CCTV-1高清"}}},
		[]tvProg{{"1", "cust", day.Add(2 * time.Hour), day.Add(3 * time.Hour)}},
	))
	src := &model.CustomSource{Name: "电信", URL: files.URL + "/epg.xml", Mode: model.CustomSourceModeFull, Enabled: 1}
	if err := customRepo.Create(context.Background(), src); err != nil {
		t.Fatal(err)
	}

	env.scheduler.SyncAllEPG(true)
	assertPrograms(t, env.today(t, "CCTV1"), src.ProviderID()+":cust")

	// 自定义源下载失败：不动数据，下一轮强制刷新后频道回到 provider 的完整数据
	files.remove("/epg.xml")
	env.scheduler.SyncAllEPG(true)
	assertPrograms(t, env.today(t, "CCTV1"), "vl:vl-a", "vl:vl-b")
	got, _ := customRepo.GetByID(context.Background(), src.ID)
	if got.LastStatus != model.CustomSourceStatusFailed {
		t.Errorf("status = %q, want failed", got.LastStatus)
	}
}

// 零点补抓（不强制）：当天只有增量源的节目时，内置源照常抓取并换掉它们；
// 之后增量源再同步时也不会再补这一天
func TestMidnightSyncReplacesFillPrograms(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{"CCTV1": {titles: []string{"vl-a", "vl-b"}}})
	env := newRefreshEnv(t, map[string][]string{"vl": {"CCTV1"}}, vl)

	customRepo := postgres.NewCustomSourceRepository(env.db)
	custom := NewCustomSourceService(customRepo, postgres.NewChannelRepository(env.db), env.programs, cache.NewMemoryCache())
	env.scheduler.SetCustomSources(custom)

	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	files := newFileServer(t)
	files.set("/fill.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"CCTV-1"}}},
		[]tvProg{{"1", "fill-a", day, day.Add(time.Hour)}, {"1", "fill-b", day.Add(time.Hour), day.Add(2 * time.Hour)}},
	))
	src := &model.CustomSource{Name: "补充", URL: files.URL + "/fill.xml", Mode: model.CustomSourceModeFill, Enabled: 1, Priority: 10}
	if err := customRepo.Create(context.Background(), src); err != nil {
		t.Fatal(err)
	}

	// 模拟前一天提前补进来的数据：只有增量源
	custom.SyncAll(context.Background())
	assertPrograms(t, env.today(t, "CCTV1"), src.ProviderID()+":fill-a", src.ProviderID()+":fill-b")

	env.scheduler.SyncAllEPG(false)

	if got := vl.requestedChannels(); len(got) != 1 || got[0] != "CCTV1" {
		t.Errorf("vl requested %v, want CCTV1 (fill-only day must not count as existing)", got)
	}
	assertPrograms(t, env.today(t, "CCTV1"), "vl:vl-a", "vl:vl-b")
}

// 当天已有全量源或内置源的节目时，零点补抓照旧跳过
func TestMidnightSyncSkipsDaysWithFullPrograms(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{"CCTV1": {titles: []string{"vl-a"}}})
	env := newRefreshEnv(t, map[string][]string{"vl": {"CCTV1"}}, vl)
	env.seed(t, "CCTV1", "custom:1", "full-a")

	env.scheduler.SyncAllEPG(false)

	if got := vl.requestedChannels(); len(got) != 0 {
		t.Errorf("vl requested %v for a day that already has full custom programs", got)
	}
	assertPrograms(t, env.today(t, "CCTV1"), "custom:1:full-a")
}

// gatedProvider 在 gate 关闭前阻塞每次抓取，用来制造「同步进行中」的状态；
// 每次开始抓取时向 entered 发送频道名，并记录同时在抓的最大数量
type gatedProvider struct {
	*fakeProvider
	gate    chan struct{}
	entered chan string

	mu       sync.Mutex
	inflight int
	maxIn    int
}

func newGatedProvider(id string, results map[string]fakeResult) *gatedProvider {
	return &gatedProvider{
		fakeProvider: newFakeProvider(id, 1, results),
		gate:         make(chan struct{}),
		entered:      make(chan string, 64),
	}
}

func (g *gatedProvider) FetchEPG(ctx context.Context, providerChannelID, channelID string, date time.Time) ([]*model.Program, error) {
	g.mu.Lock()
	g.inflight++
	g.maxIn = max(g.maxIn, g.inflight)
	g.mu.Unlock()
	g.entered <- channelID
	<-g.gate
	defer func() {
		g.mu.Lock()
		g.inflight--
		g.mu.Unlock()
	}()
	return g.fakeProvider.FetchEPG(ctx, providerChannelID, channelID, date)
}

func (g *gatedProvider) FetchEPGBatch(ctx context.Context, infos []*model.ChannelMappingInfo, date time.Time) ([]*model.Program, error) {
	return g.BaseProvider.FetchEPGBatch(ctx, g, infos, date)
}

func waitEntered(t *testing.T, g *gatedProvider) string {
	t.Helper()
	select {
	case ch := <-g.entered:
		return ch
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not asked to fetch")
		return ""
	}
}

func waitRefillIdle(t *testing.T, s *SchedulerService) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.refillMu.Lock()
		idle := !s.refillRunning
		s.refillMu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("refill did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 两个全量同步同时触发：第二个等第一个结束再开始，不会并行抓取，也不会把同一批节目写两遍
func TestSyncAllEPGRunsOneAtATime(t *testing.T) {
	g := newGatedProvider("vl", map[string]fakeResult{"CCTV1": {titles: []string{"a", "b"}}})
	env := newRefreshEnv(t, map[string][]string{"vl": {"CCTV1"}}, g)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			env.scheduler.SyncAllEPG(false)
		}()
	}
	waitEntered(t, g)
	// 给第二个同步足够的时间：没有互斥的话，它会在这期间也开始抓取
	time.Sleep(200 * time.Millisecond)
	close(g.gate)
	wg.Wait()

	if g.maxIn != 1 {
		t.Errorf("max concurrent fetches = %d, want 1", g.maxIn)
	}
	assertPrograms(t, env.today(t, "CCTV1"), "vl:a", "vl:b")
}

// 补抓只处理传入的频道，其它当天同样缺数据的频道不会被顺带抓取
func TestRefillChannelsOnlyFetchesGivenChannels(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{
		"CCTV1": {titles: []string{"a"}},
		"CCTV2": {titles: []string{"x"}},
	})
	env := newRefreshEnv(t, map[string][]string{"vl": {"CCTV1", "CCTV2"}}, vl)

	env.scheduler.RefillChannels([]string{"CCTV1"})
	waitRefillIdle(t, env.scheduler)

	if got := vl.requestedChannels(); len(got) != 1 || got[0] != "CCTV1" {
		t.Errorf("requested %v, want only CCTV1", got)
	}
	assertPrograms(t, env.today(t, "CCTV1"), "vl:a")
	assertPrograms(t, env.today(t, "CCTV2"))
}

// 补抓进行中又来了两次请求：它们合并成一轮，而不是各跑一轮。
// 每轮结束都会同步一次自定义源，用自定义源文件被下载的次数数出轮数
func TestRefillChannelsCoalesces(t *testing.T) {
	g := newGatedProvider("vl", map[string]fakeResult{
		"CCTV1": {titles: []string{"a"}},
		"CCTV2": {titles: []string{"x"}},
		"CCTV3": {titles: []string{"y"}},
	})
	env := newRefreshEnv(t, map[string][]string{"vl": {"CCTV1", "CCTV2", "CCTV3"}}, g)

	files := newFileServer(t)
	files.set("/fill.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"本地台"}}}, nil))
	customRepo := postgres.NewCustomSourceRepository(env.db)
	custom := NewCustomSourceService(customRepo, postgres.NewChannelRepository(env.db), env.programs, cache.NewMemoryCache())
	env.scheduler.SetCustomSources(custom)
	if err := customRepo.Create(context.Background(), &model.CustomSource{
		Name: "补充", URL: files.URL + "/fill.xml", Mode: model.CustomSourceModeFill, Enabled: 1,
	}); err != nil {
		t.Fatal(err)
	}

	env.scheduler.RefillChannels([]string{"CCTV1"})
	if ch := waitEntered(t, g); ch != "CCTV1" {
		t.Fatalf("first round fetched %s", ch)
	}
	// 第一轮还卡在抓取中
	env.scheduler.RefillChannels([]string{"CCTV2"})
	env.scheduler.RefillChannels([]string{"CCTV3", "CCTV2"})
	close(g.gate)
	waitRefillIdle(t, env.scheduler)

	if got := g.requestedChannels(); strings.Join(got, ",") != "CCTV1,CCTV2,CCTV3" {
		t.Errorf("requested %v, want each of CCTV1..3 once", got)
	}
	if n := files.hitCount("/fill.xml"); n != 2 {
		t.Errorf("refill rounds = %d, want 2 (the two later calls merged into one round)", n)
	}
	if g.maxIn > 2 {
		t.Errorf("max concurrent fetches = %d, rounds overlapped", g.maxIn)
	}
}

// 删除全量源后，只补抓它占用过的频道，其它当天同样缺数据的频道不受影响
func TestDeleteFullSourceRefillsOnlyItsChannels(t *testing.T) {
	vl := newFakeProvider("vl", 1, map[string]fakeResult{
		"CCTV1": {titles: []string{"vl-a"}},
		"CCTV2": {titles: []string{"vl-x"}},
	})
	env := newRefreshEnv(t, map[string][]string{"vl": {"CCTV1", "CCTV2"}}, vl)

	customRepo := postgres.NewCustomSourceRepository(env.db)
	custom := NewCustomSourceService(customRepo, postgres.NewChannelRepository(env.db), env.programs, cache.NewMemoryCache())
	env.scheduler.SetCustomSources(custom)
	custom.SetRefill(env.scheduler.RefillChannels)

	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	files := newFileServer(t)
	files.set("/full.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"CCTV-1"}}},
		[]tvProg{{"1", "cust", day.Add(2 * time.Hour), day.Add(3 * time.Hour)}},
	))
	src := &model.CustomSource{Name: "电信", URL: files.URL + "/full.xml", Mode: model.CustomSourceModeFull, Enabled: 1}
	if err := customRepo.Create(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	custom.SyncAll(context.Background())
	assertPrograms(t, env.today(t, "CCTV1"), src.ProviderID()+":cust")

	if err := custom.Delete(context.Background(), src.ID); err != nil {
		t.Fatal(err)
	}
	waitRefillIdle(t, env.scheduler)

	if got := vl.requestedChannels(); len(got) != 1 || got[0] != "CCTV1" {
		t.Errorf("requested %v, want only CCTV1", got)
	}
	assertPrograms(t, env.today(t, "CCTV1"), "vl:vl-a")
	assertPrograms(t, env.today(t, "CCTV2"))
}
