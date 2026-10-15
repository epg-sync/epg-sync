package service

import (
	"testing"

	"github.com/epg-sync/epgsync/internal/model"
)

func TestFindBestMatchHomeplus(t *testing.T) {
	s := &ChannelMappingService{}
	standard := []*model.Channel{
		{ChannelID: "五星体育", DisplayName: "五星体育"},
		{ChannelID: "纬来体育", DisplayName: "纬来体育"},
	}
	pc := &model.ProviderChannel{ID: "720-72", Name: "緯來體育", Aliases: []string{"纬来体育"}}

	got, score := s.findBestMatch(pc, standard)
	if got == nil || got.ChannelID != "纬来体育" {
		t.Fatalf("matched %v, want 纬来体育", got)
	}
	// AutoMapChannels 只保存分数 >= 0.8 的映射
	if score < 0.8 {
		t.Errorf("score = %.2f, below the 0.8 mapping threshold", score)
	}
}
