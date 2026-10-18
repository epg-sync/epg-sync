package vl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
)

// testdata 是从浏览器抓包（HAR）中保存的真实页面
func readPage(t *testing.T, day string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/SPT_" + day + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func taipei(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		t.Fatal(err)
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParsePageRollsOverMidnight(t *testing.T) {
	entries := parsePage(readPage(t, "2026-10-20"), taipei(t, "2026-10-20 00:00"))
	if len(entries) != 20 {
		t.Fatalf("got %d entries, want 20", len(entries))
	}

	checks := []struct {
		i        int
		start    string
		title    string
		subtitle string
	}{
		{0, "2026-10-20 07:00", "2026 WRC世界拉力賽分站精華", "#13"},
		{2, "2026-10-20 08:10", "2026-2027 NFL美式足球 LIVE", "華盛頓指揮官VS舊金山49人(10/20/26)#27"},
		{7, "2026-10-20 15:00", "2026 MLB美國職棒", "守護者VS光芒(10/20/26)#211-美聯冠軍G6(如有需要)"},
		{5, "2026-10-20 12:00", "2026凱基證券緯來運動STUDIO", ""},
		{14, "2026-10-20 23:00", "2026名古屋亞運", "射擊決賽-男子不定向飛靶(楊昆弼銀牌)(9/29/26)#5"},
		// 页面上 23:00 之后的行属于第二天
		{15, "2026-10-21 00:00", "2026 CPBL台灣大賽", "味全VS兄弟(10/20/26)#3-總冠軍賽G3"},
		{19, "2026-10-21 06:30", "2026棒球週報", "#41 (調整)"},
	}
	for _, c := range checks {
		e := entries[c.i]
		if !e.start.Equal(taipei(t, c.start)) || e.title != c.title || e.subtitle != c.subtitle {
			t.Errorf("entry %d = {%s %q %q}, want {%s %q %q}",
				c.i, e.start.Format("2006-01-02 15:04"), e.title, e.subtitle, c.start, c.title, c.subtitle)
		}
	}
}

// 正在播出的那一行样式不同（class="black28"），也要能解析
func TestParsePageIncludesHighlightedRow(t *testing.T) {
	entries := parsePage(readPage(t, "2026-10-18"), taipei(t, "2026-10-18 00:00"))
	if len(entries) != 19 {
		t.Fatalf("got %d entries, want 19", len(entries))
	}
	e := entries[1]
	if !e.start.Equal(taipei(t, "2026-10-18 07:30")) || e.title != "2026名古屋亞運" || e.subtitle != "桌球決賽-林昀儒(9/23)#7" {
		t.Errorf("highlighted row parsed as %+v", e)
	}
}

// 节目异动的行副标题前没有 <br/>，副标题仍要和节目名分开
func TestParsePageChangedProgramRow(t *testing.T) {
	entries := parsePage(readPage(t, "2026-10-18"), taipei(t, "2026-10-18 00:00"))
	e := entries[7]
	if !e.start.Equal(taipei(t, "2026-10-18 17:00")) ||
		e.title != "2026 CPBL台灣大賽 LIVE" ||
		e.subtitle != "兄弟VS味全(10/18/26)#2-總冠軍賽G2 [節目異動]" {
		t.Errorf("changed row parsed as {%s %q %q}", e.start.Format("15:04"), e.title, e.subtitle)
	}
}

// fakeSite 按 wdt 返回 testdata 中的页面，并记录收到的查询参数
type fakeSite struct {
	mu       sync.Mutex
	requests []string
}

func (f *fakeSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.URL.Query().Get("udt")+"|"+r.URL.Query().Get("wdt")+"|"+r.URL.Query().Get("kd"))
	f.mu.Unlock()

	if r.URL.Path != listPath {
		http.NotFound(w, r)
		return
	}
	day := strings.ReplaceAll(r.URL.Query().Get("wdt"), "/", "-")
	b, err := os.ReadFile("testdata/" + r.URL.Query().Get("kd") + "_" + day + ".html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(b)
}

func newTestProvider(t *testing.T) (*VLProvider, *fakeSite) {
	t.Helper()
	site := &fakeSite{}
	srv := httptest.NewServer(site)
	t.Cleanup(srv.Close)

	p, err := New(&model.ProviderConfig{ID: "vl", BaseURL: srv.URL, Enabled: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return p.(*VLProvider), site
}

// 10/20 当天的节目：00:00~07:00 来自 10/19 的页面末尾，07:00 以后来自 10/20 的页面
func TestFetchEPGCombinesBroadcastDays(t *testing.T) {
	p, site := newTestProvider(t)

	programs, err := p.FetchEPG(context.Background(), "SPT", "纬来体育", taipei(t, "2026-10-20 00:00"))
	if err != nil {
		t.Fatal(err)
	}

	// 10/19 页面凌晨的 4 个节目 + 10/20 页面 07:00~23:00 的 15 个节目
	if len(programs) != 19 {
		for _, pr := range programs {
			t.Log(pr.StartTime.Format("01-02 15:04"), pr.EndTime.Format("01-02 15:04"), pr.Title)
		}
		t.Fatalf("got %d programs, want 19", len(programs))
	}

	want := []struct {
		i          int
		start, end string
		title      string
	}{
		// 10/19 22:00 开始、跨过午夜的节目属于 10/19，不出现在这里
		{0, "2026-10-20 01:00", "2026-10-20 01:30", "2026凱基證券緯來運動STUDIO"},
		// 上一页最后一个节目播到本页第一个节目开始
		{3, "2026-10-20 04:30", "2026-10-20 07:00", "2026~27日本職業桌球T-League"},
		{4, "2026-10-20 07:00", "2026-10-20 08:00", "2026 WRC世界拉力賽分站精華"},
		// 当天最后一个节目播到次日 00:00（下一行），保留真实的结束时间
		{18, "2026-10-20 23:00", "2026-10-21 00:00", "2026名古屋亞運"},
	}
	for _, w := range want {
		pr := programs[w.i]
		if !pr.StartTime.Equal(taipei(t, w.start)) || !pr.EndTime.Equal(taipei(t, w.end)) || pr.Title != w.title {
			t.Errorf("program %d = {%s %s %q}, want {%s %s %q}", w.i,
				pr.StartTime.Format("2006-01-02 15:04"), pr.EndTime.Format("2006-01-02 15:04"), pr.Title,
				w.start, w.end, w.title)
		}
		if pr.ChannelID != "纬来体育" || pr.ProviderID != "vl" || pr.OriginalTimezone != timezone {
			t.Errorf("program %d metadata = %+v", w.i, pr)
		}
	}
	for i := 1; i < len(programs); i++ {
		if programs[i].StartTime.Before(programs[i-1].EndTime) {
			t.Errorf("programs %d and %d overlap", i-1, i)
		}
	}

	// udt 是所在周的周一，和页面上的链接一致
	wantRequests := []string{"2026/10/19|2026/10/19|SPT", "2026/10/19|2026/10/20|SPT"}
	if strings.Join(site.requests, ",") != strings.Join(wantRequests, ",") {
		t.Errorf("requests = %v, want %v", site.requests, wantRequests)
	}
}

// 前一天的页面取不到时，仍返回当天 07:00 以后的节目
func TestFetchEPGWithoutPreviousPage(t *testing.T) {
	p, _ := newTestProvider(t)

	// testdata 里没有 10/17 的页面；10/18 页面上午夜前有 12 个节目
	programs, err := p.FetchEPG(context.Background(), "SPT", "纬来体育", taipei(t, "2026-10-18 00:00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(programs) != 12 || !programs[0].StartTime.Equal(taipei(t, "2026-10-18 07:00")) {
		t.Fatalf("got %d programs starting %v, want 12 starting 07:00", len(programs), programs[0].StartTime)
	}
}

// 周日的页面属于上一周：取 10/19（周一）时，前一天 10/18 的 udt 是 10/12
func TestFetchEPGWeekParameter(t *testing.T) {
	p, site := newTestProvider(t)
	if _, err := p.FetchEPG(context.Background(), "SPT", "纬来体育", taipei(t, "2026-10-19 00:00")); err != nil {
		t.Fatal(err)
	}
	want := "2026/10/12|2026/10/18|SPT,2026/10/19|2026/10/19|SPT"
	if got := strings.Join(site.requests, ","); got != want {
		t.Errorf("requests = %s, want %s", got, want)
	}
}

func TestFetchEPGMissingDay(t *testing.T) {
	p, _ := newTestProvider(t)
	if _, err := p.FetchEPG(context.Background(), "SPT", "纬来体育", taipei(t, "2026-11-01 00:00")); err == nil {
		t.Fatal("expected error for a day without a page")
	}
}
