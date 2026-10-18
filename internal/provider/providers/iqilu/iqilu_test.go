package iqilu

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
)

func TestNormalizeClock(t *testing.T) {
	cases := map[string]string{
		"06:30:00": "06:30:00",
		"6:30":     "06:30:00",
		"0:00":     "00:00:00",
		"23:59":    "23:59:00",
		"8:05:09":  "08:05:09",
		"06：30：00": "06:30:00",
		" 7:15 ":   "07:15:00",
		"24:00":    "23:59:59",
		"24:00:00": "23:59:59",
	}
	for in, want := range cases {
		got, err := normalizeClock(in)
		if err != nil || got != want {
			t.Errorf("normalizeClock(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "6", "6:3:0:0", "ab:cd", "25:00", "24:30", "6:60", "123:00"} {
		if got, err := normalizeClock(bad); err == nil {
			t.Errorf("normalizeClock(%q) = %q, want error", bad, got)
		}
	}
}

// testdata 是 2026-10-18 山东农科（channelID=30）的真实返回，时间格式是 "H:MM"，
// 之前按 HH:MM:SS 解析全部失败，当天没有节目
func TestParseEPGResponseShortTimeFormat(t *testing.T) {
	data, err := os.ReadFile("testdata/channel30_2026-10-18.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(&model.ProviderConfig{ID: "iqilu", BaseURL: "http://unused"})
	if err != nil {
		t.Fatal(err)
	}

	programs, err := p.(*IQiluProvider).ParseEPGResponse(data, "30", "山东农科", "2026-10-18")
	if err != nil {
		t.Fatal(err)
	}
	if len(programs) != 51 {
		t.Fatalf("got %d programs, want 51", len(programs))
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	first, last := programs[0], programs[len(programs)-1]
	if !first.StartTime.Equal(time.Date(2026, 10, 18, 0, 0, 0, 0, loc)) ||
		!first.EndTime.Equal(time.Date(2026, 10, 18, 0, 52, 0, 0, loc)) ||
		first.Title != "乡村季风+种地宝典" {
		t.Errorf("first program = %s-%s %q", first.StartTime, first.EndTime, first.Title)
	}
	if !last.StartTime.Equal(time.Date(2026, 10, 18, 23, 28, 0, 0, loc)) || last.Title != "田间示范秀" {
		t.Errorf("last program = %s %q", last.StartTime, last.Title)
	}
	for i := 1; i < len(programs); i++ {
		if !programs[i].StartTime.Equal(programs[i-1].EndTime) {
			t.Errorf("gap or overlap between %d and %d: %s / %s", i-1, i, programs[i-1].EndTime, programs[i].StartTime)
		}
		if strings.TrimSpace(programs[i].Title) != programs[i].Title {
			t.Errorf("title not trimmed: %q", programs[i].Title)
		}
	}
}
