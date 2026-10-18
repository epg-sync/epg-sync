package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const baseConfig = `
server:
  timeout: 120
`

// clearEnv 清掉会影响配置加载的环境变量，避免受开发机环境干扰
func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"APP_ENV", "JWT_SECRET", "ADMIN_PASSWORD", "POSTGRES_HOST", "POSTGRES_PORT",
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_SSLMODE"} {
		t.Setenv(name, "")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadFromString(t *testing.T, content string) (*AppConfig, error) {
	t.Helper()
	clearEnv(t)
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", content)
	return loadFrom(dir)
}

func TestDefaultsWithoutFiles(t *testing.T) {
	clearEnv(t)
	cfg, err := loadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Env != EnvProd || len(cfg.LoadedFiles) != 0 {
		t.Errorf("env = %q, files = %v", cfg.Env, cfg.LoadedFiles)
	}
	if cfg.Server.Port != 5678 || cfg.Server.Mode != "release" || cfg.Server.Timeout != 90 || cfg.Server.JWTExpireHours != 24 {
		t.Errorf("unexpected server defaults: %+v", cfg.Server)
	}
	if cfg.Logger.Level != "info" || cfg.Logger.Output != "stdout" || cfg.Logger.Filename != "logs/epgsync.log" {
		t.Errorf("unexpected logger defaults: %+v", cfg.Logger)
	}
	// 与 Postgres 官方镜像的默认值一致：用户 postgres，库名同用户名
	if cfg.Database.URL != "postgres://postgres@localhost:5432/postgres?sslmode=disable" {
		t.Errorf("default url = %q", cfg.Database.URL)
	}
	if cfg.Database.Target() != "postgres@localhost:5432/postgres" {
		t.Errorf("target = %q", cfg.Database.Target())
	}
}

func TestDatabaseFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("POSTGRES_USER", "epg")
	t.Setenv("POSTGRES_PASSWORD", "p@ss/w:rd?")
	t.Setenv("POSTGRES_HOST", "db")
	t.Setenv("POSTGRES_PORT", "6543")
	t.Setenv("POSTGRES_SSLMODE", "require")

	cfg, err := loadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatalf("built url does not parse: %v", err)
	}
	pw, _ := u.User.Password()
	// 未设置 POSTGRES_DB 时库名取用户名
	if u.User.Username() != "epg" || pw != "p@ss/w:rd?" || u.Host != "db:6543" ||
		u.Path != "/epg" || u.Query().Get("sslmode") != "require" {
		t.Errorf("unexpected url %q", cfg.Database.URL)
	}

	t.Setenv("POSTGRES_DB", "epg_sync")
	cfg, _ = loadFrom(t.TempDir())
	if cfg.Database.Name != "epg_sync" {
		t.Errorf("name = %q, want epg_sync", cfg.Database.Name)
	}
}

func TestInvalidPostgresPort(t *testing.T) {
	clearEnv(t)
	t.Setenv("POSTGRES_PORT", "abc")

	if _, err := loadFrom(t.TempDir()); err == nil || !strings.Contains(err.Error(), "POSTGRES_PORT") {
		t.Fatalf("expected POSTGRES_PORT error, got %v", err)
	}
}

func TestJWTSecretFromEnvOnly(t *testing.T) {
	clearEnv(t)
	t.Setenv("JWT_SECRET", "s3cret")
	cfg, err := loadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.JWTSecret != "s3cret" {
		t.Errorf("jwt secret = %q", cfg.Server.JWTSecret)
	}
}

func TestLayeredConfigFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
server:
  timeout: 120
logger:
  level: warn
providers:
  ysp:
    enabled: false
  homeplus:
    priority: 1
`)
	writeFile(t, dir, "config.dev.yaml", `
logger:
  level: debug
providers:
  homeplus:
    rate_limit: 3
`)

	clearEnv(t)
	prod, err := loadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(prod.LoadedFiles) != 1 || prod.Logger.Level != "warn" || prod.Server.Timeout != 120 {
		t.Errorf("prod: files=%v level=%q timeout=%d", prod.LoadedFiles, prod.Logger.Level, prod.Server.Timeout)
	}

	t.Setenv("APP_ENV", "dev")
	dev, err := loadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.LoadedFiles) != 2 || dev.Server.Mode != "development" {
		t.Errorf("dev: files=%v mode=%q", dev.LoadedFiles, dev.Server.Mode)
	}
	// dev 文件覆盖同名项，没写的项保留 config.yaml 的值
	if dev.Logger.Level != "debug" || dev.Server.Timeout != 120 || dev.Logger.Output != "stdout" {
		t.Errorf("dev: level=%q timeout=%d output=%q", dev.Logger.Level, dev.Server.Timeout, dev.Logger.Output)
	}
	// providers 按字段合并
	if ysp := dev.Providers["ysp"]; ysp.Enabled == nil || *ysp.Enabled {
		t.Errorf("ysp override from config.yaml was lost")
	}
	hp := dev.Providers["homeplus"]
	if hp.Priority == nil || *hp.Priority != 1 || hp.RateLimit == nil || *hp.RateLimit != 3 {
		t.Errorf("homeplus not merged: priority=%v rate_limit=%v", hp.Priority, hp.RateLimit)
	}
}

func TestUnknownAppEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "staging")
	if _, err := loadFrom(t.TempDir()); err == nil {
		t.Error("expected error for unknown APP_ENV")
	}
}

// 已改为环境变量的项、拼错的键写进配置文件时应直接报错，而不是被静默忽略
func TestStrictRejectsMisplacedKeys(t *testing.T) {
	for _, content := range []string{
		"database:\n  host: db\n",
		"server:\n  jwt_secret: x\n",
		"cache:\n  type: redis\n",
		"logger:\n  levle: debug\n",
	} {
		_, err := loadFromString(t, content)
		if err == nil || !strings.Contains(err.Error(), "environment variables") {
			t.Errorf("%q: expected strict parse error, got %v", content, err)
		}
	}
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
