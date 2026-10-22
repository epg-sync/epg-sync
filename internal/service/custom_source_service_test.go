package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/database/dbtest"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/internal/repository/postgres"
	"github.com/epg-sync/epgsync/internal/xmltv"
	"github.com/epg-sync/epgsync/pkg/errors"
)

// ---- 测试用的 XMLTV 文件服务 ----

type tvChannel struct {
	id    string
	names []string
	icon  string
}

type tvProg struct {
	channel, title string
	from, to       time.Time
}

func buildXMLTV(channels []tvChannel, progs []tvProg) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><tv>`)
	for _, c := range channels {
		fmt.Fprintf(&b, `<channel id="%s">`, c.id)
		for _, n := range c.names {
			fmt.Fprintf(&b, `<display-name>%s</display-name>`, n)
		}
		if c.icon != "" {
			fmt.Fprintf(&b, `<icon src="%s"/>`, c.icon)
		}
		b.WriteString(`</channel>`)
	}
	layout := "20060102150405 -0700"
	for _, p := range progs {
		fmt.Fprintf(&b, `<programme start="%s" stop="%s" channel="%s"><title>%s</title></programme>`,
			p.from.In(xmltv.DefaultLocation).Format(layout), p.to.In(xmltv.DefaultLocation).Format(layout), p.channel, p.title)
	}
	b.WriteString(`</tv>`)
	return b.String()
}

// fileServer 按路径提供可随时替换内容的文件，路径以 .gz 结尾时压缩后返回
type fileServer struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string]string
	gate  chan struct{} // 非 nil 时，请求会阻塞到它被关闭
}

func newFileServer(t *testing.T) *fileServer {
	t.Helper()
	fs := &fileServer{files: map[string]string{}}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		content, ok := fs.files[r.URL.Path]
		gate := fs.gate
		fs.mu.Unlock()
		if gate != nil {
			<-gate
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".gz") {
			var buf bytes.Buffer
			gw := gzip.NewWriter(&buf)
			gw.Write([]byte(content))
			gw.Close()
			w.Write(buf.Bytes())
			return
		}
		w.Write([]byte(content))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fileServer) set(path, content string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.files[path] = content
}

func (fs *fileServer) remove(path string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	delete(fs.files, path)
}

// ---- 测试环境 ----

type customEnv struct {
	svc      *CustomSourceService
	repo     repository.CustomSourceRepository
	programs repository.ProgramRepository
	channels repository.ChannelRepository
	cache    cache.Cache
	files    *fileServer
	base     time.Time // 频道时区（东八区）今天 00:00，与机器时区无关
}

func newCustomEnv(t *testing.T) *customEnv {
	t.Helper()
	db := dbtest.New(t)
	e := &customEnv{
		repo:     postgres.NewCustomSourceRepository(db),
		programs: postgres.NewProgramRepository(db),
		channels: postgres.NewChannelRepository(db),
		cache:    cache.NewMemoryCache(),
		files:    newFileServer(t),
	}
	e.svc = NewCustomSourceService(e.repo, e.channels, e.programs, e.cache)
	loc, err := time.LoadLocation(defaultChannelTimezone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(loc)
	e.base = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return e
}

// h 返回今天 00:00 之后第 n 个小时；n >= 24 落在明天
func (e *customEnv) h(n int) time.Time { return e.base.Add(time.Duration(n) * time.Hour) }

func (e *customEnv) addSource(t *testing.T, path string, priority, enabled int) *model.CustomSource {
	return e.add(t, path, model.CustomSourceModeFull, priority, enabled)
}

func (e *customEnv) addFill(t *testing.T, path string, priority int) *model.CustomSource {
	return e.add(t, path, model.CustomSourceModeFill, priority, 1)
}

func (e *customEnv) add(t *testing.T, path, mode string, priority, enabled int) *model.CustomSource {
	t.Helper()
	src := &model.CustomSource{Name: "src" + path, URL: e.files.URL + path, Mode: mode, Enabled: enabled, Priority: priority}
	if err := e.repo.Create(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	return src
}

// seedBuiltin 给频道写入今天 24 个整点节目 b0..b23，模拟内置 provider 的数据
func (e *customEnv) seedBuiltin(t *testing.T, channelID string) {
	t.Helper()
	e.seedBuiltinDay(t, channelID, 0, "b")
}

// seedBuiltinDay 给频道写入今天之后第 offset 天的 24 个整点节目，标题为 prefix+小时
func (e *customEnv) seedBuiltinDay(t *testing.T, channelID string, offset int, prefix string) {
	t.Helper()
	var programs []*model.Program
	for i := range 24 {
		programs = append(programs, &model.Program{
			ChannelID: channelID, Title: fmt.Sprintf("%s%d", prefix, i), ProviderID: "cctv_cn",
			StartTime: e.h(offset*24 + i), EndTime: e.h(offset*24 + i + 1),
		})
	}
	if err := e.programs.CreateBatch(context.Background(), programs); err != nil {
		t.Fatal(err)
	}
}

// listed 返回频道所有节目，格式 "provider:title"，按开始时间排序
func (e *customEnv) listed(t *testing.T, channelID string) []string {
	t.Helper()
	return e.listedBetween(t, channelID, e.base.AddDate(0, 0, -30), e.base.AddDate(0, 0, 30))
}

// listedDay 返回频道今天之后第 offset 天的节目
func (e *customEnv) listedDay(t *testing.T, channelID string, offset int) []string {
	t.Helper()
	day := e.base.AddDate(0, 0, offset)
	return e.listedBetween(t, channelID, day, day.AddDate(0, 0, 1))
}

func (e *customEnv) listedBetween(t *testing.T, channelID string, from, to time.Time) []string {
	t.Helper()
	ps, err := e.programs.ListByChannelIDAndTimeRange(context.Background(), channelID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ProviderID + ":" + p.Title
	}
	return out
}

func (e *customEnv) source(t *testing.T, id int64) *model.CustomSource {
	t.Helper()
	src, err := e.repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func waitStatus(t *testing.T, e *customEnv, id int64, want string) *model.CustomSource {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		src := e.source(t, id)
		if src.LastStatus == want {
			return src
		}
		if time.Now().After(deadline) {
			t.Fatalf("source %d status = %q (error %q), want %q", id, src.LastStatus, src.LastError, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitPrograms 等频道的节目变成 want（后台同步是异步的）
func waitPrograms(t *testing.T, e *customEnv, channelID string, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := e.listed(t, channelID)
		if strings.Join(got, ",") == strings.Join(want, ",") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %q, want %q", channelID, got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// hourly 生成某个频道从第 from 小时到第 to 小时的整点节目，标题为 prefix+小时
func hourly(channel, prefix string, e *customEnv, from, to int) []tvProg {
	var out []tvProg
	for i := from; i < to; i++ {
		out = append(out, tvProg{channel, fmt.Sprintf("%s%d", prefix, i), e.h(i), e.h(i + 1)})
	}
	return out
}

func titles(provider, prefix string, from, to int) []string {
	var out []string
	for i := from; i < to; i++ {
		out = append(out, fmt.Sprintf("%s:%s%d", provider, prefix, i))
	}
	return out
}

// ---- 全量覆盖 ----

// 全量源按「频道+天」整天覆盖：当天只要有文件里的节目，内置节目整天被换掉（包括文件没覆盖的时段）；
// 文件没有的天保持不变
func TestCustomSourceFullReplacesWholeDayAndCreatesChannels(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.seedBuiltin(t, "CCTV1")
	e.seedBuiltinDay(t, "CCTV1", 1, "t")
	e.seedBuiltin(t, "CCTV2")

	before, err := e.channels.GetByID(ctx, "CCTV1")
	if err != nil {
		t.Fatal(err)
	}
	all, err := e.channels.GetAllChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	channelCount := len(all)

	e.files.set("/epg.xml.gz", buildXMLTV(
		[]tvChannel{
			{id: "1001", names: []string{"CCTV-1高清"}},                                 // 名称正则匹配到内置 CCTV1
			{id: "CCTV2", names: []string{"中央二台"}},                                    // channel_id 精确匹配
			{id: "2002", names: []string{"广东本地台高清"}, icon: "http://logo.test/gd.png"}, // 内置没有，新建
		},
		[]tvProg{
			{"1001", "c8", e.h(8), e.h(9)}, {"1001", "c9", e.h(9), e.h(10)}, {"1001", "c20", e.h(20), e.h(21)},
			{"CCTV2", "x8", e.h(8), e.h(9)},
			{"2002", "l8", e.h(8), e.h(9)}, {"2002", "l9", e.h(9), e.h(10)},
		},
	))
	src := e.addSource(t, "/epg.xml.gz", 0, 1)
	pid := src.ProviderID()

	// 缓存的 XMLTV 输出要在同步后失效
	e.cache.Set(ctx, xmltvCacheKey(time.Now()), "stale", time.Hour)

	e.svc.SyncAll(ctx)

	// 今天整天以文件为准，节目之间的空档也不保留内置节目；明天文件里没有，内置节目不动
	assertPrograms(t, e.listedDay(t, "CCTV1", 0), pid+":c8", pid+":c9", pid+":c20")
	assertPrograms(t, e.listedDay(t, "CCTV1", 1), titles("cctv_cn", "t", 0, 24)...)
	assertPrograms(t, e.listedDay(t, "CCTV2", 0), pid+":x8")

	// 新建的频道：名称去掉清晰度后缀，带台标，且按名称（含 HD 写法）能被 DIYP 的正则查到
	local, err := e.channels.GetByID(ctx, "广东本地台")
	if err != nil {
		t.Fatalf("local channel not created: %v", err)
	}
	if local.DisplayName != "广东本地台高清" || local.LogoURL != "http://logo.test/gd.png" {
		t.Errorf("local channel = %+v", local)
	}
	for _, name := range []string{"广东本地台", "广东本地台高清", "广东本地台 HD"} {
		if ch, err := e.channels.GetByChannelName(ctx, name); err != nil || ch.ChannelID != "广东本地台" {
			t.Errorf("GetByChannelName(%q) = %+v, %v", name, ch, err)
		}
	}
	assertPrograms(t, e.listed(t, "广东本地台"), pid+":l8", pid+":l9")

	// 匹配到的内置频道不能被修改，也不能多出重复频道
	after, _ := e.channels.GetByID(ctx, "CCTV1")
	if after.DisplayName != before.DisplayName || after.Regexp != before.Regexp || after.LogoURL != before.LogoURL {
		t.Errorf("built-in channel modified: before=%+v after=%+v", before, after)
	}
	if all, _ := e.channels.GetAllChannels(ctx); len(all) != channelCount+1 {
		t.Errorf("channels = %d, want %d", len(all), channelCount+1)
	}

	got := e.source(t, src.ID)
	if got.LastStatus != model.CustomSourceStatusSuccess || got.LastError != "" || got.LastMessage != "" ||
		got.ProgramCount != 6 || got.LastSyncAt == nil {
		t.Errorf("source state = %+v", got)
	}
	if ok, _ := e.cache.Exists(ctx, xmltvCacheKey(time.Now())); ok {
		t.Error("xmltv cache was not invalidated")
	}
}

func TestCustomSourceResyncIsIdempotentAndDropsStale(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}

	// 今天三个节目，明天一个
	e.files.set("/epg.xml", buildXMLTV(ch, []tvProg{
		{"1", "a", e.h(8), e.h(9)}, {"1", "b", e.h(9), e.h(10)}, {"1", "c", e.h(10), e.h(11)},
		{"1", "tomorrow", e.h(32), e.h(33)},
	}))
	src := e.addSource(t, "/epg.xml", 0, 1)
	pid := src.ProviderID()

	e.svc.SyncAll(ctx)
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "本地台"), pid+":a", pid+":b", pid+":c", pid+":tomorrow")

	// 文件内容变短，且不再有明天：上次写入、这次文件里没有的节目和天都不能残留
	e.files.set("/epg.xml", buildXMLTV(ch, []tvProg{
		{"1", "b2", e.h(9), e.h(10)}, {"1", "c", e.h(10), e.h(11)},
	}))
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "本地台"), pid+":b2", pid+":c")
	if got := e.source(t, src.ID); got.ProgramCount != 2 {
		t.Errorf("program_count = %d, want 2", got.ProgramCount)
	}
}

func TestCustomSourceExpiredOrBrokenFileKeepsExistingData(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.seedBuiltin(t, "CCTV1")

	old := time.Date(2020, 1, 1, 8, 0, 0, 0, time.UTC)
	e.files.set("/expired.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"CCTV-1"}}},
		[]tvProg{{"1", "old", old, old.Add(time.Hour)}},
	))
	e.files.set("/broken.xml", `<tv><programme start="x"`)

	cases := map[string]string{
		"/expired.xml": "没有可用节目",
		"/broken.xml":  "parse",
		"/missing.xml": "404",
	}
	for path, wantErr := range cases {
		src := e.addSource(t, path, 0, 1)
		e.svc.SyncAll(ctx)

		got := e.source(t, src.ID)
		if got.LastStatus != model.CustomSourceStatusFailed || !strings.Contains(got.LastError, wantErr) {
			t.Errorf("%s: status=%q error=%q, want failed containing %q", path, got.LastStatus, got.LastError, wantErr)
		}
		if err := e.repo.Delete(ctx, src.ID); err != nil {
			t.Fatal(err)
		}
	}

	assertPrograms(t, e.listed(t, "CCTV1"), titles("cctv_cn", "b", 0, 24)...)
}

// 多个全量源争同一个「频道+天」：先添加的优先，不论谁的数据多；后添加的只写别人没占的天
func TestCustomSourceFullOlderWins(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
	e.files.set("/old.xml", buildXMLTV(ch, []tvProg{{"1", "O", e.h(8), e.h(9)}}))
	e.files.set("/new.xml", buildXMLTV(ch, append(hourly("1", "n", e, 0, 24), tvProg{"1", "N-tomorrow", e.h(30), e.h(31)})))
	older := e.addSource(t, "/old.xml", 0, 1)
	newer := e.addSource(t, "/new.xml", 0, 1)

	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listedDay(t, "本地台", 0), older.ProviderID()+":O")
	assertPrograms(t, e.listedDay(t, "本地台", 1), newer.ProviderID()+":N-tomorrow")
	got := e.source(t, newer.ID)
	if got.LastStatus != model.CustomSourceStatusSuccess || !strings.Contains(got.LastMessage, "1 个频道日已被先添加的全量源占用") {
		t.Errorf("newer state = %+v", got)
	}

	// 先添加的源这次下载失败：它上次的数据保留，后添加的源仍然不能占用那一天
	e.files.remove("/old.xml")
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listedDay(t, "本地台", 0), older.ProviderID()+":O")
	if got := e.source(t, older.ID); got.LastStatus != model.CustomSourceStatusFailed || got.ProgramCount != 1 {
		t.Errorf("older state = %+v", got)
	}

	// 先添加的源被删除后，后添加的源接管这一天
	if err := e.svc.Delete(ctx, older.ID); err != nil {
		t.Fatal(err)
	}
	want := append(titles(newer.ProviderID(), "n", 0, 24), newer.ProviderID()+":N-tomorrow")
	waitPrograms(t, e, "本地台", want...)
}

func TestCustomSourceDisabledIsIgnored(t *testing.T) {
	e := newCustomEnv(t)
	e.files.set("/epg.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"本地台"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}},
	))
	src := e.addSource(t, "/epg.xml", 0, 0)

	e.svc.SyncAll(context.Background())

	if got := e.listed(t, "本地台"); len(got) != 0 {
		t.Errorf("disabled source wrote programs: %q", got)
	}
	if got := e.source(t, src.ID); got.LastStatus != "" || got.LastSyncAt != nil {
		t.Errorf("disabled source was synced: %+v", got)
	}
}

// 删除全量源：清除它的节目，并触发补抓内置节目
func TestCustomSourceDeleteFullPurgesAndRefills(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	refilled := make(chan struct{}, 1)
	e.svc.SetRefill(func() { refilled <- struct{}{} })

	e.seedBuiltin(t, "CCTV1")
	e.files.set("/epg.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"CCTV-1"}}, {id: "2", names: []string{"本地台"}}},
		[]tvProg{{"1", "c", e.h(8), e.h(9)}, {"2", "l", e.h(8), e.h(9)}},
	))
	src := e.addSource(t, "/epg.xml", 0, 1)
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "CCTV1"), src.ProviderID()+":c")

	if err := e.svc.Delete(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.GetByID(ctx, src.ID); !errors.Is(err, errors.ErrCodeNotFound) {
		t.Errorf("source still exists: %v", err)
	}
	// 全量源占过的那天已经没有数据，要等补抓
	assertPrograms(t, e.listed(t, "CCTV1"))
	assertPrograms(t, e.listed(t, "本地台"))
	select {
	case <-refilled:
	case <-time.After(time.Second):
		t.Fatal("refill was not triggered after deleting a full source")
	}
}

func TestCustomSourceStartSyncIsExclusiveAndAsync(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.files.set("/epg.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"本地台"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}},
	))
	e.files.gate = make(chan struct{})
	src := e.addSource(t, "/epg.xml", 0, 1)

	if err := e.svc.StartSync(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	// 下载被卡住期间：状态已经是 running，再次同步 / 删除都被拒绝
	if got := e.source(t, src.ID); got.LastStatus != model.CustomSourceStatusRunning {
		t.Errorf("status = %q, want running", got.LastStatus)
	}
	if err := e.svc.StartSync(ctx, src.ID); !errors.Is(err, errors.ErrCodeAlreadyExists) {
		t.Errorf("second StartSync err = %v, want ErrSyncInProgress", err)
	}
	if err := e.svc.Delete(ctx, src.ID); !errors.Is(err, errors.ErrCodeAlreadyExists) {
		t.Errorf("Delete during sync err = %v, want ErrSyncInProgress", err)
	}

	close(e.files.gate)
	waitStatus(t, e, src.ID, model.CustomSourceStatusSuccess)
	assertPrograms(t, e.listed(t, "本地台"), src.ProviderID()+":a")

	if err := e.svc.StartSync(ctx, 999999); !errors.Is(err, errors.ErrCodeNotFound) {
		t.Errorf("StartSync on missing source err = %v", err)
	}
}

func TestCustomSourceCreateAndUpdateTriggerSync(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.files.set("/a.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"本地台"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}},
	))
	e.files.set("/b.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"本地台"}}},
		[]tvProg{{"1", "b", e.h(8), e.h(9)}},
	))

	src, err := e.svc.Create(ctx, &model.CustomSource{Name: " 电信 ", URL: e.files.URL + "/a.xml", Mode: model.CustomSourceModeFull, Enabled: 1})
	if err != nil {
		t.Fatal(err)
	}
	if src.Name != "电信" {
		t.Errorf("name not trimmed: %q", src.Name)
	}
	waitStatus(t, e, src.ID, model.CustomSourceStatusSuccess)
	assertPrograms(t, e.listed(t, "本地台"), src.ProviderID()+":a")

	// 改了地址会重新同步
	updated, err := e.svc.Update(ctx, &model.CustomSource{ID: src.ID, Name: "电信", URL: e.files.URL + "/b.xml", Mode: model.CustomSourceModeFull, Enabled: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitPrograms(t, e, "本地台", updated.ProviderID()+":b")
}

func TestCustomSourceValidation(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	full := model.CustomSourceModeFull
	for _, src := range []*model.CustomSource{
		{Name: "", URL: "http://example.com/a.xml", Mode: full},
		{Name: "x", URL: "", Mode: full},
		{Name: "x", URL: "ftp://example.com/a.xml", Mode: full},
		{Name: "x", URL: "file:///etc/passwd", Mode: full},
		{Name: "x", URL: "example.com/a.xml", Mode: full},
		{Name: "x", URL: "http://example.com/a.xml", Mode: "replace"},
		{Name: "x", URL: "http://example.com/a.xml"},
	} {
		if _, err := e.svc.Create(ctx, src); !errors.Is(err, errors.ErrCodeInvalidParam) {
			t.Errorf("Create(%+v) err = %v, want invalid param", src, err)
		}
	}
}

func TestResetStale(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	src := e.addSource(t, "/epg.xml", 0, 1)
	if err := e.repo.SetRunning(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ResetStale(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.source(t, src.ID); got.LastStatus != model.CustomSourceStatusFailed || got.LastError == "" {
		t.Errorf("stale running state not reset: %+v", got)
	}
}

func TestNormalizeChannelName(t *testing.T) {
	cases := map[string]string{
		"CCTV-1高清":    "CCTV-1",
		"CCTV-1 高清":   "CCTV-1",
		"广东卫视 HD":     "广东卫视",
		"广东体育-高清":     "广东体育",
		"纪实超高清":       "纪实",
		" 凤凰卫视中文台 ":   "凤凰卫视中文台",
		"CCTV-4K 超高清": "CCTV-4K", // 4K 是独立频道，不能被继续削掉
		"高清":          "高清",      // 全是后缀时保持原样
		"北京卫视":        "北京卫视",
	}
	for in, want := range cases {
		if got := normalizeChannelName(in); got != want {
			t.Errorf("normalizeChannelName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomSourceStartSyncRejectsDisabled(t *testing.T) {
	e := newCustomEnv(t)
	src := e.addSource(t, "/a.xml", 0, 0)
	if err := e.svc.StartSync(context.Background(), src.ID); !errors.Is(err, errors.ErrCodeInvalidParam) {
		t.Errorf("StartSync on disabled source err = %v, want invalid param", err)
	}
}

// 只有一个全量源时停用：节目被清除，并触发补抓内置节目
func TestCustomSourceDisableFullPurgesAndRefills(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	refilled := make(chan struct{}, 1)
	e.svc.SetRefill(func() { refilled <- struct{}{} })

	e.seedBuiltin(t, "CCTV1")
	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"CCTV-1"}}},
		[]tvProg{{"1", "c8", e.h(8), e.h(9)}}))
	src := e.addSource(t, "/a.xml", 0, 1)
	e.svc.SyncAll(ctx)

	src.Enabled = 0
	updated, err := e.svc.Update(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled != 0 || updated.LastStatus != "" || updated.ProgramCount != 0 {
		t.Errorf("disabled source state = %+v, want enabled=0, status empty, program_count 0", updated)
	}
	assertPrograms(t, e.listed(t, "CCTV1"))
	select {
	case <-refilled:
	case <-time.After(time.Second):
		t.Fatal("refill was not triggered after disabling a full source")
	}

	// 之后的同步也不会再写入停用的源
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "CCTV1"))
}

func TestCustomSourceDisableDuringSyncConflicts(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"本地台"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}}))
	e.files.gate = make(chan struct{})
	src := e.addSource(t, "/a.xml", 0, 1)
	if err := e.svc.StartSync(ctx, src.ID); err != nil {
		t.Fatal(err)
	}

	src.Enabled = 0
	if _, err := e.svc.Update(ctx, src); !errors.Is(err, errors.ErrCodeAlreadyExists) {
		t.Errorf("disable during sync err = %v, want ErrSyncInProgress", err)
	}
	if got := e.source(t, src.ID); got.Enabled != 1 {
		t.Errorf("source was disabled despite the conflict")
	}
	close(e.files.gate)
	waitStatus(t, e, src.ID, model.CustomSourceStatusSuccess)
}

// 文件里去掉的频道，上次写入的节目要清掉，空出来的天可以被增量源补上
func TestCustomSourceRemovedChannelIsCleaned(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.files.set("/a.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"台A"}}, {id: "2", names: []string{"台B"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}, {"2", "b", e.h(8), e.h(9)}}))
	e.files.set("/other.xml", buildXMLTV(
		[]tvChannel{{id: "x", names: []string{"台B"}}},
		[]tvProg{{"x", "other", e.h(12), e.h(13)}}))
	src := e.addSource(t, "/a.xml", 0, 1)
	other := e.addFill(t, "/other.xml", 10)
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "台B"), src.ProviderID()+":b")

	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"台A"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}}))
	e.svc.SyncAll(ctx)

	assertPrograms(t, e.listed(t, "台A"), src.ProviderID()+":a")
	assertPrograms(t, e.listed(t, "台B"), other.ProviderID()+":other")
}

// 宽松的前缀正则不能把不同的频道合并进来，但真正同一频道的各种写法仍要认出来
func TestCustomSourceChannelMatching(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()

	cases := []struct {
		name string
		want string // 期望对应到的 channel_id
	}{
		{"广东卫视国际", "广东卫视国际"}, // ^广东卫视 前缀匹配，但「国际」是另一个频道 -> 新建
		{"北京卫视4K", "北京卫视4K"}, // 4K 是独立频道 -> 新建
		{"广东卫视高清", "广东卫视"},
		{"CCTV-5+ 体育赛事", "CCTV5+"}, // 剩下的「体育赛事」是频道名本来就有的
		{"CCTV1 综合频道", "CCTV1"},
		{"CCTV-1 综合", "CCTV1"}, // display_name 精确匹配
		{"cctv13", "CCTV13"},
	}
	var channels []tvChannel
	var progs []tvProg
	for i, c := range cases {
		id := fmt.Sprintf("c%d", i)
		channels = append(channels, tvChannel{id: id, names: []string{c.name}})
		// 每个用例一个时段，几个名字归到同一频道时不会被当成重复节目去掉
		progs = append(progs, tvProg{id, c.name, e.h(i), e.h(i + 1)})
	}
	e.files.set("/a.xml", buildXMLTV(channels, progs))
	src := e.addSource(t, "/a.xml", 0, 1)
	e.svc.SyncAll(ctx)

	if got := e.source(t, src.ID); got.LastStatus != model.CustomSourceStatusSuccess {
		t.Fatalf("sync failed: %s", got.LastError)
	}
	for _, c := range cases {
		got := e.listed(t, c.want)
		if !slices.Contains(got, src.ProviderID()+":"+c.name) {
			t.Errorf("%q: channel %s has %q, want it to hold this programme", c.name, c.want, got)
		}
	}
	// 被前缀正则误匹配的内置频道不能收到别的频道的节目
	for _, id := range []string{"广东卫视", "北京卫视"} {
		for _, p := range e.listed(t, id) {
			if strings.Contains(p, "国际") || strings.Contains(p, "4K") {
				t.Errorf("%s got programme of another channel: %s", id, p)
			}
		}
	}
}

func TestRegexpRemainderOK(t *testing.T) {
	cctv5p := &model.Channel{ChannelID: "CCTV5+", DisplayName: "CCTV-5 体育赛事"}
	gd := &model.Channel{ChannelID: "广东卫视", DisplayName: "广东卫视"}
	cases := []struct {
		rest string
		ch   *model.Channel
		want bool
	}{
		{"", gd, true},
		{"高清", gd, true},
		{" HD", gd, true},
		{"-超高清", gd, true},
		{" 频道", gd, true},
		{"频道 高清", gd, true},
		{"国际", gd, false},
		{"4K", gd, false},
		{" 体育赛事", cctv5p, true},
		{"体育赛事 HD", cctv5p, true},
		{"足球", cctv5p, false},
	}
	for _, c := range cases {
		if got := regexpRemainderOK(c.rest, c.ch); got != c.want {
			t.Errorf("regexpRemainderOK(%q, %s) = %v, want %v", c.rest, c.ch.ChannelID, got, c.want)
		}
	}
}

// ---- 增量补充 ----

// 增量源只补内置源当天没有节目的「频道+天」
func TestCustomSourceFillOnlyFillsMissingDays(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.seedBuiltin(t, "CCTV1")
	e.files.set("/fill.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"CCTV-1"}}, {id: "2", names: []string{"本地台"}}},
		[]tvProg{
			{"1", "f-today", e.h(8), e.h(9)},      // 内置今天有 -> 不补
			{"1", "f-tomorrow", e.h(32), e.h(33)}, // 内置明天没有 -> 补
			{"2", "l", e.h(8), e.h(9)},            // 内置没有这个频道 -> 补
		}))
	src := e.addFill(t, "/fill.xml", 10)
	pid := src.ProviderID()
	if !strings.HasPrefix(pid, model.CustomSourceFillProviderPrefix) {
		t.Fatalf("fill provider id = %q", pid)
	}

	e.svc.SyncAll(ctx)

	assertPrograms(t, e.listedDay(t, "CCTV1", 0), titles("cctv_cn", "b", 0, 24)...)
	assertPrograms(t, e.listedDay(t, "CCTV1", 1), pid+":f-tomorrow")
	assertPrograms(t, e.listed(t, "本地台"), pid+":l")
	got := e.source(t, src.ID)
	if got.LastStatus != model.CustomSourceStatusSuccess || got.ProgramCount != 2 ||
		got.LastMessage != "补充了 2 个频道日，1 个频道日已有节目" {
		t.Errorf("source state = %+v", got)
	}
}

// 增量源不会覆盖全量源，不论优先级多高
func TestCustomSourceFillNeverOverridesFull(t *testing.T) {
	e := newCustomEnv(t)
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
	e.files.set("/full.xml", buildXMLTV(ch, []tvProg{{"1", "F", e.h(8), e.h(9)}}))
	e.files.set("/fill.xml", buildXMLTV(ch, []tvProg{{"1", "f", e.h(10), e.h(11)}}))
	fill := e.addFill(t, "/fill.xml", 0) // 先添加、优先级最高
	full := e.addSource(t, "/full.xml", 0, 1)

	e.svc.SyncAll(context.Background())
	e.svc.SyncAll(context.Background())

	assertPrograms(t, e.listed(t, "本地台"), full.ProviderID()+":F")
	if got := e.source(t, fill.ID); got.ProgramCount != 0 {
		t.Errorf("fill source wrote %d programs", got.ProgramCount)
	}
}

// 增量源之间按优先级：数字小的先补，不论添加顺序；低优先级的只补剩下的天
func TestCustomSourceFillPriority(t *testing.T) {
	for _, highFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("highFirst=%v", highFirst), func(t *testing.T) {
			e := newCustomEnv(t)
			ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
			e.files.set("/high.xml", buildXMLTV(ch, []tvProg{{"1", "H", e.h(8), e.h(10)}}))
			e.files.set("/low.xml", buildXMLTV(ch, []tvProg{{"1", "L", e.h(8), e.h(10)}, {"1", "L-tomorrow", e.h(32), e.h(33)}}))

			var high, low *model.CustomSource
			if highFirst {
				high = e.addFill(t, "/high.xml", 1)
				low = e.addFill(t, "/low.xml", 5)
			} else {
				low = e.addFill(t, "/low.xml", 5)
				high = e.addFill(t, "/high.xml", 1)
			}

			e.svc.SyncAll(context.Background())
			assertPrograms(t, e.listed(t, "本地台"), high.ProviderID()+":H", low.ProviderID()+":L-tomorrow")
		})
	}
}

// 高优先级增量源某次下载失败：它上次补的天保留，低优先级的不会趁机占用
func TestCustomSourceFillHigherFailureKeepsItsDays(t *testing.T) {
	e := newCustomEnv(t)
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
	e.files.set("/high.xml", buildXMLTV(ch, []tvProg{{"1", "H", e.h(8), e.h(10)}}))
	e.files.set("/low.xml", buildXMLTV(ch, []tvProg{{"1", "L", e.h(8), e.h(10)}}))
	high := e.addFill(t, "/high.xml", 1)
	e.addFill(t, "/low.xml", 5)
	e.svc.SyncAll(context.Background())

	e.files.remove("/high.xml")
	e.svc.SyncAll(context.Background())

	assertPrograms(t, e.listed(t, "本地台"), high.ProviderID()+":H")
}

// 「立即同步」优先级低的增量源，不能让它盖掉优先级高的
func TestCustomSourceManualSyncKeepsPriority(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
	e.files.set("/high.xml", buildXMLTV(ch, []tvProg{{"1", "H", e.h(8), e.h(10)}}))
	e.files.set("/low.xml", buildXMLTV(ch, []tvProg{{"1", "L", e.h(8), e.h(10)}}))
	high := e.addFill(t, "/high.xml", 1)
	low := e.addFill(t, "/low.xml", 5)

	if err := e.svc.StartSync(ctx, low.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, e, low.ID, model.CustomSourceStatusSuccess)
	waitStatus(t, e, high.ID, model.CustomSourceStatusSuccess)
	assertPrograms(t, e.listed(t, "本地台"), high.ProviderID()+":H")
}

// 停用增量源：清除它补的节目，低优先级增量源接手
func TestCustomSourceDisableFillRestoresLower(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
	e.files.set("/high.xml", buildXMLTV(ch, []tvProg{{"1", "H", e.h(8), e.h(10)}}))
	e.files.set("/low.xml", buildXMLTV(ch, []tvProg{{"1", "L", e.h(8), e.h(10)}}))
	high := e.addFill(t, "/high.xml", 1)
	low := e.addFill(t, "/low.xml", 5)
	e.svc.SyncAll(ctx)

	high.Enabled = 0
	if _, err := e.svc.Update(ctx, high); err != nil {
		t.Fatal(err)
	}
	waitPrograms(t, e, "本地台", low.ProviderID()+":L")
	waitStatus(t, e, low.ID, model.CustomSourceStatusSuccess)
}

// 切换模式：先清除按原模式写入的节目，再按新模式同步
func TestCustomSourceModeSwitch(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.seedBuiltinDay(t, "CCTV1", 1, "t") // 内置只有明天
	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"CCTV-1"}}},
		[]tvProg{{"1", "today", e.h(8), e.h(9)}, {"1", "tomorrow", e.h(32), e.h(33)}}))
	src := e.addSource(t, "/a.xml", 0, 1)
	full := src.ProviderID()
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "CCTV1"), full+":today", full+":tomorrow")

	// 全量 -> 增量：明天内置有数据，不补（内置明天的数据已被全量源覆盖过，等补抓）；今天补上
	src.Mode = model.CustomSourceModeFill
	updated, err := e.svc.Update(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	fill := updated.ProviderID()
	if fill == full || !strings.HasPrefix(fill, model.CustomSourceFillProviderPrefix) {
		t.Fatalf("provider id after switch = %q", fill)
	}
	waitPrograms(t, e, "CCTV1", fill+":today", fill+":tomorrow")

	// 增量 -> 全量
	updated.Mode = model.CustomSourceModeFull
	if _, err := e.svc.Update(ctx, updated); err != nil {
		t.Fatal(err)
	}
	waitPrograms(t, e, "CCTV1", full+":today", full+":tomorrow")
}

func TestSourceOrder(t *testing.T) {
	full := model.CustomSourceModeFull
	fill := model.CustomSourceModeFill
	sources := []*model.CustomSource{
		{ID: 1, Mode: fill, Priority: 5},
		{ID: 2, Mode: full, Priority: 1}, // 全量源的 priority 不起作用
		{ID: 3, Mode: fill, Priority: 1},
		{ID: 4, Mode: full, Priority: 0},
		{ID: 5, Mode: fill, Priority: 1},
	}
	var got []int64
	for _, s := range sourceOrder(sources) {
		got = append(got, s.ID)
	}
	if want := []int64{2, 4, 3, 5, 1}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
