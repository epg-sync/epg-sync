package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const baseConfig = `
server:
  port: 5678
database:
  driver: sqlite
  host: localhost
  port: 3306
  user: root
  password: x
  name: /tmp/epg.db
`

func loadFromString(t *testing.T, content string) (*AppConfig, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", path)
	return LoadConfig()
}

func TestProvidersEmpty(t *testing.T) {
	for _, providers := range []string{"", "providers:\n", "providers: {}\n"} {
		cfg, err := loadFromString(t, baseConfig+providers)
		if err != nil {
			t.Fatalf("%q: %v", providers, err)
		}
		if len(cfg.Providers) != 0 {
			t.Errorf("%q: got %d overrides, want 0", providers, len(cfg.Providers))
		}
	}
}

func TestProvidersOverrideMap(t *testing.T) {
	cfg, err := loadFromString(t, baseConfig+`
providers:
  ysp:
    enabled: false
  homeplus:
    priority: 1
    timeout: 15s
`)
	if err != nil {
		t.Fatal(err)
	}

	ysp := cfg.Providers["ysp"]
	if ysp.Enabled == nil || *ysp.Enabled {
		t.Errorf("ysp.enabled = %v, want false", ysp.Enabled)
	}
	if ysp.Priority != nil {
		t.Errorf("ysp.priority should be unset, got %v", *ysp.Priority)
	}

	hp := cfg.Providers["homeplus"]
	if hp.Priority == nil || *hp.Priority != 1 {
		t.Errorf("homeplus.priority = %v, want 1", hp.Priority)
	}
	if hp.Timeout == nil || *hp.Timeout != 15*time.Second {
		t.Errorf("homeplus.timeout = %v, want 15s", hp.Timeout)
	}
}

func TestProvidersLegacyListRejected(t *testing.T) {
	_, err := loadFromString(t, baseConfig+`
providers:
  - name: ysp
    id: ysp
    enabled: true
`)
	if err == nil || !strings.Contains(err.Error(), "INSTALL.md") {
		t.Fatalf("expected migration hint error, got %v", err)
	}
}
