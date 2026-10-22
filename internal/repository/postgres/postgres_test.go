package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/database"
	"github.com/epg-sync/epgsync/internal/database/dbtest"
	"github.com/epg-sync/epgsync/internal/model"
)

func TestMigrateSeedsData(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	var channels, timezones int64
	db.Model(&model.Channel{}).Count(&channels)
	db.Model(&model.Timezone{}).Count(&timezones)
	if channels != 190 || timezones != 564 {
		t.Fatalf("got %d channels, %d timezones; want 190, 564", channels, timezones)
	}

	// 再次执行迁移不应报错，也不应重复写入
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	db.Model(&model.Channel{}).Count(&channels)
	if channels != 190 {
		t.Fatalf("channels after re-migrate = %d, want 190", channels)
	}
}

func TestSeedRegexpsValid(t *testing.T) {
	db := dbtest.New(t)

	// 任何一条 regexp 不合法都会让整条查询报错
	var n int64
	err := db.Model(&model.Channel{}).Where("regexp <> '' AND ? ~* regexp", "probe").Count(&n).Error
	if err != nil {
		t.Fatalf("invalid regexp in seed data: %v", err)
	}
}

func TestGetByChannelName(t *testing.T) {
	db := dbtest.New(t)
	repo := NewChannelRepository(db)
	ctx := context.Background()

	cases := map[string]string{
		"cctv1":        "CCTV1",
		"CCTV-1 综合":    "CCTV1",
		"CCTV-2":       "CCTV2",
		"cctv-5 体育":    "CCTV5",
		"CCTV-6 电影":    "CCTV6",
		"纬来体育":         "纬来体育",
		"緯來體育台":        "纬来体育",
		"CCTV1高清":      "CCTV1",
		"CCTV13":       "CCTV13",
		"CCTV-10 科教":   "CCTV10",
		"CCTV5+":       "CCTV5+",
		"CCTV-5+ 体育赛事": "CCTV5+",
		"CCTV5体育赛事":    "CCTV5+",
		"CCTV-4 中文国际":  "CCTV4",
		"CCTV4K超高清":    "CCTV4K",
		"CCTV-4 欧洲":    "CCTV4欧洲",
		"上海五星体育":       "五星体育",
	}
	for name, want := range cases {
		ch, err := repo.GetByChannelName(ctx, name)
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if ch.ChannelID != want {
			t.Errorf("%q: got %s, want %s", name, ch.ChannelID, want)
		}
	}
}

// 每个内置频道用自己的 channel_id 查找时都应命中自己，避免正则互相覆盖
func TestEverySeedChannelResolvesToItself(t *testing.T) {
	db := dbtest.New(t)
	repo := NewChannelRepository(db)
	ctx := context.Background()

	channels, err := repo.GetAllChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range channels {
		if want.Regexp == "" {
			continue
		}
		got, err := repo.GetByChannelName(ctx, want.ChannelID)
		if err != nil {
			t.Errorf("%s: %v", want.ChannelID, err)
			continue
		}
		if got.ChannelID != want.ChannelID {
			t.Errorf("%s resolved to %s", want.ChannelID, got.ChannelID)
		}
	}
}

func TestChannelCreateBatchUpserts(t *testing.T) {
	db := dbtest.New(t)
	repo := NewChannelRepository(db)
	ctx := context.Background()

	err := repo.CreateBatch(ctx, []*model.Channel{
		{ChannelID: "CCTV1", DisplayName: "renamed", Timezone: "Asia/Shanghai"},
		{ChannelID: "TEST1", DisplayName: "test", Timezone: "Asia/Shanghai"},
	})
	if err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	ch, err := repo.GetByID(ctx, "CCTV1")
	if err != nil {
		t.Fatal(err)
	}
	if ch.DisplayName != "renamed" {
		t.Errorf("CCTV1 display_name = %q, want renamed", ch.DisplayName)
	}

	count, _ := repo.Count(ctx)
	if count != 191 {
		t.Errorf("count = %d, want 191", count)
	}
}

func TestMappingCreateUpserts(t *testing.T) {
	db := dbtest.New(t)
	repo := NewChannelMappingsRepository(db)
	ctx := context.Background()

	first := &model.ChannelMapping{ProviderID: "p", ProviderChannelID: "x", CanonicalID: "CCTV1", Confidence: 0.5}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := &model.ChannelMapping{ProviderID: "p", ProviderChannelID: "x", CanonicalID: "CCTV2", Confidence: 0.9}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("second Create should upsert: %v", err)
	}

	mappings, err := repo.ListByProviderID(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || mappings[0].CanonicalID != "CCTV2" || mappings[0].Confidence != 0.9 {
		t.Fatalf("unexpected mappings: %+v", mappings)
	}
}

func TestProgramBatchAndDeleteBefore(t *testing.T) {
	db := dbtest.New(t)
	repo := NewProgramRepository(db)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	programs := make([]*model.Program, 0, 2500)
	for i := range 2500 {
		st := start.Add(time.Duration(i) * time.Minute)
		programs = append(programs, &model.Program{
			ChannelID:        "CCTV1",
			Title:            "p",
			StartTime:        st,
			EndTime:          st.Add(time.Minute),
			ProviderID:       "test",
			OriginalTimezone: "Asia/Shanghai",
		})
	}
	if err := repo.CreateBatch(ctx, programs); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	var count int64
	db.Model(&model.Program{}).Count(&count)
	if count != 2500 {
		t.Fatalf("count = %d, want 2500", count)
	}

	// end_time 早于 start+1000min 的是前 999 条
	deleted, err := repo.DeleteBefore(ctx, start.Add(1000*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 999 {
		t.Errorf("deleted = %d, want 999", deleted)
	}
}

func titlesOf(t *testing.T, repo interface {
	ListByChannelIDAndTimeRange(ctx context.Context, channelID string, start, end time.Time) ([]*model.Program, error)
}, channelID string) []string {
	t.Helper()
	programs, err := repo.ListByChannelIDAndTimeRange(context.Background(), channelID,
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(programs))
	for i, p := range programs {
		out[i] = p.ProviderID + ":" + p.Title
	}
	return out
}

func assertTitles(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestReplaceChannelRanges(t *testing.T) {
	db := dbtest.New(t)
	repo := NewProgramRepository(db)
	ctx := context.Background()

	day := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	hour := func(h int) time.Time { return day.Add(time.Duration(h) * time.Hour) }
	mk := func(channel, provider, title string, from, to int) *model.Program {
		return &model.Program{ChannelID: channel, Title: title, ProviderID: provider, StartTime: hour(from), EndTime: hour(to)}
	}
	rg := func(from, to int) model.TimeRange { return model.TimeRange{From: hour(from), To: hour(to)} }

	seed := []*model.Program{
		mk("CCTV1", "builtin", "early", 0, 6),
		mk("CCTV1", "builtin", "straddle", 6, 9),
		mk("CCTV1", "builtin", "mid", 9, 10),
		mk("CCTV1", "builtin", "gap-a", 10, 12),
		mk("CCTV1", "builtin", "gap-b", 12, 14),
		mk("CCTV1", "builtin", "evening", 14, 16),
		mk("CCTV1", "builtin", "late", 16, 18),
		mk("CCTV2", "builtin", "other", 7, 8),
		// 本源上次同步留下的、这次时间段之外的旧节目
		mk("CCTV1", "custom:1", "stale", 20, 21),
	}
	if err := repo.CreateBatch(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// 这次覆盖两段：07-10 和 14-16，中间 10-14 是空档。
	// 与这两段重叠的 straddle、mid、evening 被删；early、late 只是相邻，空档里的 gap-a、gap-b 都必须保留；
	// 本源的 stale 也要清掉；另一个频道不受影响
	write := func() {
		t.Helper()
		err := repo.ReplaceChannelRanges(ctx, "CCTV1", []model.TimeRange{rg(7, 10), rg(14, 16)}, "custom:1", hour(0), []*model.Program{
			mk("CCTV1", "custom:1", "new-a", 7, 8),
			mk("CCTV1", "custom:1", "new-b", 8, 10),
			mk("CCTV1", "custom:1", "new-c", 14, 16),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	write()
	want := []string{"builtin:early", "custom:1:new-a", "custom:1:new-b", "builtin:gap-a", "builtin:gap-b", "custom:1:new-c", "builtin:late"}
	assertTitles(t, titlesOf(t, repo, "CCTV1"), want...)
	assertTitles(t, titlesOf(t, repo, "CCTV2"), "builtin:other")

	// 重复执行结果不变（幂等）
	write()
	assertTitles(t, titlesOf(t, repo, "CCTV1"), want...)
}

// 时间段很多时要拆成多条 DELETE，不能超过参数上限
func TestReplaceChannelRangesManyRanges(t *testing.T) {
	db := dbtest.New(t)
	repo := NewProgramRepository(db)
	ctx := context.Background()

	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	var builtin, custom []*model.Program
	var ranges []model.TimeRange
	for i := range 2500 {
		// 每 10 分钟一档：前 5 分钟是自定义节目，后 5 分钟是空档，空档里有一条内置节目
		s := start.Add(time.Duration(i) * 10 * time.Minute)
		custom = append(custom, &model.Program{ChannelID: "CCTV1", Title: "c", ProviderID: "custom:1", StartTime: s, EndTime: s.Add(5 * time.Minute)})
		ranges = append(ranges, model.TimeRange{From: s, To: s.Add(5 * time.Minute)})
		builtin = append(builtin, &model.Program{ChannelID: "CCTV1", Title: "b", ProviderID: "builtin", StartTime: s.Add(5 * time.Minute), EndTime: s.Add(10 * time.Minute)})
		builtin = append(builtin, &model.Program{ChannelID: "CCTV1", Title: "x", ProviderID: "builtin", StartTime: s, EndTime: s.Add(5 * time.Minute)})
	}
	if err := repo.CreateBatch(ctx, builtin); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceChannelRanges(ctx, "CCTV1", ranges, "custom:1", start, custom); err != nil {
		t.Fatal(err)
	}

	var counts []struct {
		ProviderID string
		N          int
	}
	db.Model(&model.Program{}).Select("provider_id, count(*) AS n").Group("provider_id").Order("provider_id").Scan(&counts)
	if len(counts) != 2 || counts[0].ProviderID != "builtin" || counts[0].N != 2500 || counts[1].N != 2500 {
		t.Fatalf("counts = %+v, want 2500 builtin (gaps kept) + 2500 custom", counts)
	}
}

func TestDeleteByProviderOutsideChannels(t *testing.T) {
	db := dbtest.New(t)
	repo := NewProgramRepository(db)
	ctx := context.Background()

	now := time.Now()
	mk := func(channel, provider, title string, end time.Time) *model.Program {
		return &model.Program{ChannelID: channel, Title: title, ProviderID: provider, StartTime: end.Add(-time.Hour), EndTime: end}
	}
	if err := repo.CreateBatch(ctx, []*model.Program{
		mk("CCTV1", "custom:1", "keep", now.Add(time.Hour)),
		mk("CCTV2", "custom:1", "drop", now.Add(time.Hour)),
		mk("CCTV2", "custom:1", "old", now.Add(-72*time.Hour)), // 早于 after，交给定期清理
		mk("CCTV2", "cctv_cn", "other", now.Add(time.Hour)),
		mk("CCTV2", "custom:10", "other-source", now.Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}

	n, err := repo.DeleteByProviderOutsideChannels(ctx, "custom:1", []string{"CCTV1"}, now.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	assertTitles(t, titlesOf(t, repo, "CCTV1"), "custom:1:keep")
	assertTitles(t, titlesOf(t, repo, "CCTV2"), "custom:1:old", "cctv_cn:other", "custom:10:other-source")
}

func TestDeleteByProviderID(t *testing.T) {
	db := dbtest.New(t)
	repo := NewProgramRepository(db)
	ctx := context.Background()

	now := time.Now()
	programs := []*model.Program{
		{ChannelID: "CCTV1", Title: "a", ProviderID: "custom:1", StartTime: now, EndTime: now.Add(time.Hour)},
		{ChannelID: "CCTV1", Title: "b", ProviderID: "custom:1", StartTime: now.Add(time.Hour), EndTime: now.Add(2 * time.Hour)},
		{ChannelID: "CCTV1", Title: "c", ProviderID: "custom:10", StartTime: now.Add(2 * time.Hour), EndTime: now.Add(3 * time.Hour)},
		{ChannelID: "CCTV1", Title: "d", ProviderID: "cctv_cn", StartTime: now.Add(3 * time.Hour), EndTime: now.Add(4 * time.Hour)},
	}
	if err := repo.CreateBatch(ctx, programs); err != nil {
		t.Fatal(err)
	}

	n, err := repo.DeleteByProviderID(ctx, "custom:1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2 (custom:10 must not match custom:1)", n)
	}
	if got := titlesOf(t, repo, "CCTV1"); len(got) != 2 {
		t.Errorf("remaining = %q", got)
	}
}

func TestMatchByRegexp(t *testing.T) {
	db := dbtest.New(t)
	repo := NewChannelRepository(db)
	ctx := context.Background()

	// 所有种子正则在外面包一层 (?i)(...) 之后都必须仍然合法，否则整条查询会报错
	var n int64
	if err := db.Model(&model.Channel{}).
		Where("regexp <> '' AND substring(? from '(?i)(' || regexp || ')') IS NOT NULL", "probe").
		Count(&n).Error; err != nil {
		t.Fatalf("seed regexp invalid when wrapped: %v", err)
	}

	cases := map[string]struct{ channel, matched string }{
		"CCTV-1高清":     {"CCTV1", "CCTV-1"},
		"cctv-1 综合":    {"CCTV1", "cctv-1 综合"}, // 匹配到的是整段，而不是第一个括号里的「 综合」
		"CCTV-5+ 体育赛事": {"CCTV5+", "CCTV-5+"},
		"广东卫视国际":       {"广东卫视", "广东卫视"},
	}
	for name, want := range cases {
		matches, err := repo.MatchByRegexp(ctx, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(matches) == 0 || matches[0].ChannelID != want.channel || matches[0].Matched != want.matched {
			t.Errorf("%s: got %+v, want %s matched %q", name, matches, want.channel, want.matched)
		}
	}

	if matches, err := repo.MatchByRegexp(ctx, "完全不存在的频道"); err != nil || len(matches) != 0 {
		t.Errorf("unknown name: %+v, %v", matches, err)
	}
}
