package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/database/dbtest"
	"github.com/epg-sync/epgsync/internal/model"
)

func seedLogs(t *testing.T, repo interface {
	CreateBatch(context.Context, []*model.Log) error
}) {
	t.Helper()
	now := time.Now()
	logs := []*model.Log{
		{Time: now.Add(-48 * time.Hour), Level: 0, Message: "old entry"},
		{Time: now.Add(-3 * time.Minute), Level: 0, Logger: "http", Message: "GET /api/diyp", Fields: map[string]any{"status": 200, "query": "ch=CCTV1"}},
		{Time: now.Add(-2 * time.Minute), Level: 1, Message: "Provider timeout", Fields: map[string]any{"provider": "homeplus"}},
		{Time: now.Add(-1 * time.Minute), Level: 2, Logger: "gorm", Message: "Database query failed", Caller: "postgres/channel.go:42", Stack: "goroutine 1"},
		{Time: now, Level: 0, Message: "100% done_ok"},
	}
	if err := repo.CreateBatch(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
}

func TestLogRepositoryFilters(t *testing.T) {
	repo := NewLogRepository(dbtest.New(t))
	ctx := context.Background()
	seedLogs(t, repo)

	messages := func(f model.LogFilter) []string {
		t.Helper()
		if f.Limit == 0 {
			f.Limit = 100
		}
		logs, err := repo.List(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(logs))
		for i, l := range logs {
			out[i] = l.Message
		}
		return out
	}
	level := func(n int8) *int8 { return &n }
	str := func(s string) *string { return &s }

	cases := []struct {
		name   string
		filter model.LogFilter
		want   []string
	}{
		{"newest first", model.LogFilter{}, []string{"100% done_ok", "Database query failed", "Provider timeout", "GET /api/diyp", "old entry"}},
		{"min level warn", model.LogFilter{MinLevel: level(1)}, []string{"Database query failed", "Provider timeout"}},
		{"logger http", model.LogFilter{Logger: str("http")}, []string{"GET /api/diyp"}},
		{"logger app (empty)", model.LogFilter{Logger: str("")}, []string{"100% done_ok", "Provider timeout", "old entry"}},
		{"keyword in fields", model.LogFilter{Keyword: "HOMEPLUS"}, []string{"Provider timeout"}},
		{"keyword in caller", model.LogFilter{Keyword: "channel.go"}, []string{"Database query failed"}},
		{"keyword wildcard is literal", model.LogFilter{Keyword: "0% d"}, []string{"100% done_ok"}},
		{"keyword underscore literal", model.LogFilter{Keyword: "e_o"}, []string{"100% done_ok"}},
		{"since", model.LogFilter{Since: time.Now().Add(-time.Hour)}, []string{"100% done_ok", "Database query failed", "Provider timeout", "GET /api/diyp"}},
		{"limit", model.LogFilter{Limit: 2}, []string{"100% done_ok", "Database query failed"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := messages(c.filter)
			if len(got) != len(c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %q, want %q", got, c.want)
				}
			}
		})
	}

	// 翻页与实时刷新
	all, _ := repo.List(ctx, model.LogFilter{Limit: 100})
	older, _ := repo.List(ctx, model.LogFilter{BeforeID: all[1].ID, Limit: 2})
	if len(older) != 2 || older[0].ID != all[2].ID || older[1].ID != all[3].ID {
		t.Errorf("before_id page wrong: %+v", older)
	}
	// after_id 取紧跟在它后面的一批（最早的几条），按新到旧返回，前端用其中最大的 id 继续拉
	newer, _ := repo.List(ctx, model.LogFilter{AfterID: all[4].ID, Limit: 2})
	if len(newer) != 2 || newer[0].ID != all[2].ID || newer[1].ID != all[3].ID {
		t.Errorf("after_id page wrong: got ids %d,%d", newer[0].ID, newer[1].ID)
	}

	// 字段和堆栈原样读回
	if all[1].Stack != "goroutine 1" || all[3].Fields["query"] != "ch=CCTV1" {
		t.Errorf("fields not round-tripped: %+v %+v", all[1], all[3])
	}
}

func TestLogRepositoryDeleteBefore(t *testing.T) {
	repo := NewLogRepository(dbtest.New(t))
	ctx := context.Background()
	seedLogs(t, repo)

	n, err := repo.DeleteBefore(ctx, time.Now().Add(-24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("deleted %d (err %v), want 1", n, err)
	}
}
