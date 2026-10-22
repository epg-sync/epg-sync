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
	base     time.Time // 今天 00:00
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
	now := time.Now()
	e.base = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return e
}

func (e *customEnv) h(n int) time.Time { return e.base.Add(time.Duration(n) * time.Hour) }

func (e *customEnv) addSource(t *testing.T, path string, priority, enabled int) *model.CustomSource {
	t.Helper()
	src := &model.CustomSource{Name: "src" + path, URL: e.files.URL + path, Enabled: enabled, Priority: priority}
	if err := e.repo.Create(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	return src
}

// seedBuiltin 给频道写入今天 24 个整点节目 b0..b23，模拟内置 provider 的数据
func (e *customEnv) seedBuiltin(t *testing.T, channelID string) {
	t.Helper()
	var programs []*model.Program
	for i := range 24 {
		programs = append(programs, &model.Program{
			ChannelID: channelID, Title: fmt.Sprintf("b%d", i), ProviderID: "cctv_cn",
			StartTime: e.h(i), EndTime: e.h(i + 1),
		})
	}
	if err := e.programs.CreateBatch(context.Background(), programs); err != nil {
		t.Fatal(err)
	}
}

// listed 返回频道所有节目，格式 "provider:title"，按开始时间排序
func (e *customEnv) listed(t *testing.T, channelID string) []string {
	t.Helper()
	ps, err := e.programs.ListByChannelIDAndTimeRange(context.Background(), channelID, e.base.AddDate(0, 0, -30), e.base.AddDate(0, 0, 30))
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

// ---- 测试 ----

func TestCustomSourceOverridesBuiltinAndCreatesChannels(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.seedBuiltin(t, "CCTV1")
	e.seedBuiltin(t, "CCTV2")

	before, err := e.channels.GetByID(ctx, "CCTV1")
	if err != nil {
		t.Fatal(err)
	}
	var channelCount int64
	if all, err := e.channels.GetAllChannels(ctx); err != nil {
		t.Fatal(err)
	} else {
		channelCount = int64(len(all))
	}

	e.files.set("/epg.xml.gz", buildXMLTV(
		[]tvChannel{
			{id: "1001", names: []string{"CCTV-1高清"}},                                 // 名称正则匹配到内置 CCTV1
			{id: "CCTV2", names: []string{"中央二台"}},                                    // channel_id 精确匹配
			{id: "2002", names: []string{"广东本地台高清"}, icon: "http://logo.test/gd.png"}, // 内置没有，新建
		},
		[]tvProg{
			{"1001", "c8", e.h(8), e.h(9)}, {"1001", "c9", e.h(9), e.h(10)}, {"1001", "c10", e.h(10), e.h(11)},
			{"CCTV2", "x8", e.h(8), e.h(9)},
			{"2002", "l8", e.h(8), e.h(9)}, {"2002", "l9", e.h(9), e.h(10)},
		},
	))
	src := e.addSource(t, "/epg.xml.gz", 10, 1)
	pid := src.ProviderID()

	// 缓存的 XMLTV 输出要在同步后失效
	e.cache.Set(ctx, xmltvCacheKey(time.Now()), "stale", time.Hour)

	e.svc.SyncAll(ctx)

	// 文件覆盖 08:00-11:00，之外的内置节目原样保留
	want := []string{}
	for i := 0; i < 8; i++ {
		want = append(want, fmt.Sprintf("cctv_cn:b%d", i))
	}
	want = append(want, pid+":c8", pid+":c9", pid+":c10")
	for i := 11; i < 24; i++ {
		want = append(want, fmt.Sprintf("cctv_cn:b%d", i))
	}
	assertPrograms(t, e.listed(t, "CCTV1"), want...)

	got2 := e.listed(t, "CCTV2")
	if len(got2) != 24 || got2[8] != pid+":x8" {
		t.Errorf("CCTV2 = %q", got2)
	}

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
	if all, _ := e.channels.GetAllChannels(ctx); int64(len(all)) != channelCount+1 {
		t.Errorf("channels = %d, want %d", len(all), channelCount+1)
	}

	got := e.source(t, src.ID)
	if got.LastStatus != model.CustomSourceStatusSuccess || got.LastError != "" || got.ProgramCount != 6 || got.LastSyncAt == nil {
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

	e.files.set("/epg.xml", buildXMLTV(ch, []tvProg{
		{"1", "a", e.h(8), e.h(9)}, {"1", "b", e.h(9), e.h(10)}, {"1", "c", e.h(10), e.h(11)},
	}))
	src := e.addSource(t, "/epg.xml", 10, 1)
	pid := src.ProviderID()

	e.svc.SyncAll(ctx)
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "本地台"), pid+":a", pid+":b", pid+":c")

	// 文件内容变短：上次写入、这次文件里没有的节目不能残留
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
		src := e.addSource(t, path, 10, 1)
		src.ProgramCount = 7 // 失败时应保留上次的节目数
		e.svc.SyncAll(ctx)

		got := e.source(t, src.ID)
		if got.LastStatus != model.CustomSourceStatusFailed || !strings.Contains(got.LastError, wantErr) {
			t.Errorf("%s: status=%q error=%q, want failed containing %q", path, got.LastStatus, got.LastError, wantErr)
		}
	}

	if got := e.listed(t, "CCTV1"); len(got) != 24 || got[0] != "cctv_cn:b0" {
		t.Errorf("existing programs changed: %d programs", len(got))
	}
}

func TestCustomSourcePriority(t *testing.T) {
	// 不论创建顺序（id 大小）如何，priority 数字小的最终胜出
	for _, highFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("highFirst=%v", highFirst), func(t *testing.T) {
			e := newCustomEnv(t)
			ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
			e.files.set("/high.xml", buildXMLTV(ch, []tvProg{{"1", "H", e.h(8), e.h(10)}}))
			e.files.set("/low.xml", buildXMLTV(ch, []tvProg{{"1", "L", e.h(9), e.h(11)}}))

			var high *model.CustomSource
			if highFirst {
				high = e.addSource(t, "/high.xml", 1, 1)
				e.addSource(t, "/low.xml", 5, 1)
			} else {
				e.addSource(t, "/low.xml", 5, 1)
				high = e.addSource(t, "/high.xml", 1, 1)
			}

			e.svc.SyncAll(context.Background())
			// 先写 low 的 09-11，再写 high 的 08-10；low 的节目与之重叠，被整个覆盖掉
			assertPrograms(t, e.listed(t, "本地台"), high.ProviderID()+":H")
		})
	}
}

func TestCustomSourceDisabledIsIgnored(t *testing.T) {
	e := newCustomEnv(t)
	e.files.set("/epg.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"本地台"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}},
	))
	src := e.addSource(t, "/epg.xml", 10, 0)

	e.svc.SyncAll(context.Background())

	if got := e.listed(t, "本地台"); len(got) != 0 {
		t.Errorf("disabled source wrote programs: %q", got)
	}
	if got := e.source(t, src.ID); got.LastStatus != "" || got.LastSyncAt != nil {
		t.Errorf("disabled source was synced: %+v", got)
	}
}

func TestCustomSourceDeletePurgesItsPrograms(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.seedBuiltin(t, "CCTV1")
	e.files.set("/epg.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"CCTV-1"}}, {id: "2", names: []string{"本地台"}}},
		[]tvProg{{"1", "c", e.h(8), e.h(9)}, {"2", "l", e.h(8), e.h(9)}},
	))
	src := e.addSource(t, "/epg.xml", 10, 1)
	e.svc.SyncAll(ctx)

	if err := e.svc.Delete(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.GetByID(ctx, src.ID); !errors.Is(err, errors.ErrCodeNotFound) {
		t.Errorf("source still exists: %v", err)
	}
	for _, id := range []string{"CCTV1", "本地台"} {
		for _, p := range e.listed(t, id) {
			if strings.HasPrefix(p, model.CustomSourceProviderPrefix) {
				t.Errorf("%s still has %s", id, p)
			}
		}
	}
	// 其它 provider 的节目不受影响
	if got := e.listed(t, "CCTV1"); len(got) != 23 {
		t.Errorf("CCTV1 has %d programs, want 23 (24 built-in minus the one covered by the deleted source)", len(got))
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
	src := e.addSource(t, "/epg.xml", 10, 1)

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

	src, err := e.svc.Create(ctx, &model.CustomSource{Name: " 电信 ", URL: e.files.URL + "/a.xml", Enabled: 1, Priority: 10})
	if err != nil {
		t.Fatal(err)
	}
	if src.Name != "电信" {
		t.Errorf("name not trimmed: %q", src.Name)
	}
	waitStatus(t, e, src.ID, model.CustomSourceStatusSuccess)
	assertPrograms(t, e.listed(t, "本地台"), src.ProviderID()+":a")

	// 改了地址会重新同步
	updated, err := e.svc.Update(ctx, &model.CustomSource{ID: src.ID, Name: "电信", URL: e.files.URL + "/b.xml", Enabled: 1, Priority: 10})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := e.listed(t, "本地台")
		if len(got) == 1 && got[0] == updated.ProviderID()+":b" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not re-synced after url change: %q", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCustomSourceValidation(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	for _, src := range []*model.CustomSource{
		{Name: "", URL: "http://example.com/a.xml"},
		{Name: "x", URL: ""},
		{Name: "x", URL: "ftp://example.com/a.xml"},
		{Name: "x", URL: "file:///etc/passwd"},
		{Name: "x", URL: "example.com/a.xml"},
	} {
		if _, err := e.svc.Create(ctx, src); !errors.Is(err, errors.ErrCodeInvalidParam) {
			t.Errorf("Create(%+v) err = %v, want invalid param", src, err)
		}
	}
}

func TestResetStale(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	src := e.addSource(t, "/epg.xml", 10, 1)
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

// 问题 2：同一频道的节目中间有空档时，空档里的内置节目必须保留
func TestCustomSourceKeepsBuiltinInGaps(t *testing.T) {
	e := newCustomEnv(t)
	e.seedBuiltin(t, "CCTV1")
	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"CCTV-1"}}},
		[]tvProg{{"1", "c8", e.h(8), e.h(9)}, {"1", "c9", e.h(9), e.h(10)}, {"1", "c20", e.h(20), e.h(21)}}))
	src := e.addSource(t, "/a.xml", 10, 1)
	pid := src.ProviderID()

	e.svc.SyncAll(context.Background())

	var want []string
	for i := range 24 {
		switch i {
		case 8:
			want = append(want, pid+":c8")
		case 9:
			want = append(want, pid+":c9")
		case 20:
			want = append(want, pid+":c20")
		default:
			want = append(want, fmt.Sprintf("cctv_cn:b%d", i))
		}
	}
	assertPrograms(t, e.listed(t, "CCTV1"), want...)
}

// 问题 3：「立即同步」优先级低的源，不能盖掉优先级高的源
func TestCustomSourceManualSyncKeepsPriority(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
	e.files.set("/high.xml", buildXMLTV(ch, []tvProg{{"1", "H", e.h(8), e.h(10)}}))
	e.files.set("/low.xml", buildXMLTV(ch, []tvProg{{"1", "L", e.h(8), e.h(10)}}))
	high := e.addSource(t, "/high.xml", 1, 1)
	low := e.addSource(t, "/low.xml", 5, 1)

	if err := e.svc.StartSync(ctx, low.ID); err != nil {
		t.Fatal(err)
	}
	// 两个源都被标记为同步中，并且都同步完成
	if got := e.source(t, high.ID); got.LastStatus != model.CustomSourceStatusRunning &&
		got.LastStatus != model.CustomSourceStatusSuccess {
		t.Errorf("high status = %q, want running or success", got.LastStatus)
	}
	waitStatus(t, e, low.ID, model.CustomSourceStatusSuccess)
	waitStatus(t, e, high.ID, model.CustomSourceStatusSuccess)
	assertPrograms(t, e.listed(t, "本地台"), high.ProviderID()+":H")
}

func TestCustomSourceStartSyncRejectsDisabled(t *testing.T) {
	e := newCustomEnv(t)
	src := e.addSource(t, "/a.xml", 10, 0)
	if err := e.svc.StartSync(context.Background(), src.ID); !errors.Is(err, errors.ErrCodeInvalidParam) {
		t.Errorf("StartSync on disabled source err = %v, want invalid param", err)
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

// 问题 4：停用后清除它写入的节目，被它覆盖的低优先级源的数据重新同步回来
func TestCustomSourceDisablePurgesAndRestoresLower(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	ch := []tvChannel{{id: "1", names: []string{"本地台"}}}
	e.files.set("/high.xml", buildXMLTV(ch, []tvProg{{"1", "H", e.h(8), e.h(10)}}))
	e.files.set("/low.xml", buildXMLTV(ch, []tvProg{{"1", "L", e.h(8), e.h(10)}}))
	high := e.addSource(t, "/high.xml", 1, 1)
	low := e.addSource(t, "/low.xml", 5, 1)
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "本地台"), high.ProviderID()+":H")

	high.Enabled = 0
	updated, err := e.svc.Update(ctx, high)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled != 0 || updated.LastStatus != "" || updated.ProgramCount != 0 {
		t.Errorf("disabled source state = %+v, want enabled=0, status empty, program_count 0", updated)
	}
	waitPrograms(t, e, "本地台", low.ProviderID()+":L")
	waitStatus(t, e, low.ID, model.CustomSourceStatusSuccess)

	// 之后的定时同步也不会再写入停用的源
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "本地台"), low.ProviderID()+":L")
}

// 只有一个源时停用：节目被清除，频道恢复成内置数据（被删的时段要等内置源下次同步补回）
func TestCustomSourceDisableOnlySource(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.seedBuiltin(t, "CCTV1")
	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"CCTV-1"}}},
		[]tvProg{{"1", "c8", e.h(8), e.h(9)}}))
	src := e.addSource(t, "/a.xml", 10, 1)
	e.svc.SyncAll(ctx)

	src.Enabled = 0
	if _, err := e.svc.Update(ctx, src); err != nil {
		t.Fatal(err)
	}
	for _, p := range e.listed(t, "CCTV1") {
		if strings.HasPrefix(p, model.CustomSourceProviderPrefix) {
			t.Fatalf("disabled source still has %s", p)
		}
	}
	if got := len(e.listed(t, "CCTV1")); got != 23 {
		t.Errorf("CCTV1 has %d programs, want 23", got)
	}
}

func TestCustomSourceDisableDuringSyncConflicts(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"本地台"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}}))
	e.files.gate = make(chan struct{})
	src := e.addSource(t, "/a.xml", 10, 1)
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

// 问题 5：文件里去掉的频道，上次写入的节目要清掉；其它源的节目不受影响
func TestCustomSourceRemovedChannelIsCleaned(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	e.files.set("/a.xml", buildXMLTV(
		[]tvChannel{{id: "1", names: []string{"台A"}}, {id: "2", names: []string{"台B"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}, {"2", "b", e.h(8), e.h(9)}}))
	e.files.set("/other.xml", buildXMLTV(
		[]tvChannel{{id: "x", names: []string{"台B"}}},
		[]tvProg{{"x", "other", e.h(12), e.h(13)}}))
	src := e.addSource(t, "/a.xml", 10, 1)
	other := e.addSource(t, "/other.xml", 20, 1)
	e.svc.SyncAll(ctx)
	assertPrograms(t, e.listed(t, "台B"), src.ProviderID()+":b", other.ProviderID()+":other")

	e.files.set("/a.xml", buildXMLTV([]tvChannel{{id: "1", names: []string{"台A"}}},
		[]tvProg{{"1", "a", e.h(8), e.h(9)}}))
	e.svc.SyncAll(ctx)

	assertPrograms(t, e.listed(t, "台A"), src.ProviderID()+":a")
	assertPrograms(t, e.listed(t, "台B"), other.ProviderID()+":other")
}

// 问题 6：宽松的前缀正则不能把不同的频道合并进来，但真正同一频道的各种写法仍要认出来
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
	src := e.addSource(t, "/a.xml", 10, 1)
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

func TestCoveredRanges(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return base.Add(time.Duration(n) * time.Hour) }
	p := func(from, to int) *model.Program { return &model.Program{StartTime: h(from), EndTime: h(to)} }

	got := coveredRanges([]*model.Program{p(1, 2), p(2, 3), p(3, 5), p(4, 6), p(8, 9), p(9, 10), p(12, 13)})
	want := []model.TimeRange{{From: h(1), To: h(6)}, {From: h(8), To: h(10)}, {From: h(12), To: h(13)}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].From.Equal(want[i].From) || !got[i].To.Equal(want[i].To) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if got := coveredRanges(nil); len(got) != 0 {
		t.Errorf("empty input: %v", got)
	}
}
