package app

import (
	"testing"

	"github.com/epg-sync/epgsync/internal/database/dbtest"
)

func TestLoadOrCreateSettingPersists(t *testing.T) {
	db := dbtest.New(t)

	calls := 0
	gen := func() (string, error) {
		calls++
		return "generated", nil
	}

	first, err := loadOrCreateSetting(db, settingJWTSecret, gen)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateSetting(db, settingJWTSecret, gen)
	if err != nil {
		t.Fatal(err)
	}

	if first != "generated" || second != "generated" {
		t.Errorf("got %q then %q, want generated twice", first, second)
	}
	if calls != 1 {
		t.Errorf("generate called %d times, want 1", calls)
	}
}
