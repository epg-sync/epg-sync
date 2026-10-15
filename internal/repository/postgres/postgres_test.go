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
