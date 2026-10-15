package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/logger"
	"gopkg.in/yaml.v2"
)

var (
	once     sync.Once
	instance *AppConfig
)

type AppConfig struct {
	Server    ServerConfig            `yaml:"server"`
	Cache     CacheConfig             `yaml:"cache"`
	Providers model.ProviderOverrides `yaml:"providers"`
	Database  DatabaseConfig          `yaml:"database"`
	Scheduler SchedulerConfig         `yaml:"scheduler"`
	Logger    logger.Config           `yaml:"logger"`
}

type ServerConfig struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Mode           string `yaml:"mode"`
	Timeout        int    `yaml:"timeout"`
	JWTSecret      string `yaml:"jwt_secret"`
	JWTExpireHours int    `yaml:"jwt_expire_hours"`
}

type CacheConfig struct {
	Type     string `yaml:"type"`
	Addr     string `yaml:"addr"`
	Password string `yaml:"password" `
	DB       int    `yaml:"db"`
	TTL      string `yaml:"ttl"`
}

type DatabaseConfig struct {
	Driver   string `yaml:"driver"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
	Timezone string `yaml:"timezone"`
	Debug    bool   `yaml:"debug"`
}

type SchedulerConfig struct {
	Enabled    bool   `yaml:"enabled"`
	UpdateCron string `yaml:"update_cron"`
}

func LoadConfig(configPath ...string) (*AppConfig, error) {
	var path string
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		path = envPath
	} else if len(configPath) > 0 && configPath[0] != "" {
		path = configPath[0]
	} else {
		path = "config/config.yaml"
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", absPath, err)
	}

	var config AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	config.setDefaults()

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

func GetInstance() *AppConfig {
	once.Do(func() {
		var err error
		instance, err = LoadConfig()
		if err != nil {
			panic(fmt.Sprintf("failed to load config: %v", err))
		}
	})
	return instance
}

func MustLoad(configPath ...string) *AppConfig {
	config, err := LoadConfig(configPath...)
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}
	instance = config
	return config
}

func (c *AppConfig) setDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Timeout == 0 {
		c.Server.Timeout = 30
	}
	if c.Cache.Type == "" {
		c.Cache.Type = "memory"
	}
	if c.Cache.TTL == "" {
		c.Cache.TTL = "24h"
	}
	if c.Logger.Format == "" {
		c.Logger.Format = "json"
	}
	if c.Logger.Output == "" {
		c.Logger.Output = "stdout"
	}
	if c.Server.JWTExpireHours == 0 {
		c.Server.JWTExpireHours = 24
	}
}

func (c *AppConfig) Validate() error {

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Cache.Type != "" && c.Cache.Type != "memory" && c.Cache.Type != "redis" {
		return fmt.Errorf("unsupported cache type: %s", c.Cache.Type)
	}
	if c.Cache.Type == "redis" && c.Cache.Addr == "" {
		return fmt.Errorf("redis address is required when cache type is redis")
	}
	if _, err := time.ParseDuration(c.Cache.TTL); err != nil {
		return fmt.Errorf("invalid cache TTL: %s", c.Cache.TTL)
	}

	if c.Database.Driver == "" {
		return fmt.Errorf("database driver is required")
	}
	switch c.Database.Driver {
	case "mysql":
		if c.Database.Host == "" {
			return fmt.Errorf("database host is required")
		}
		if c.Database.Port == 0 {
			return fmt.Errorf("database port is required")
		}
		if c.Database.User == "" {
			return fmt.Errorf("database user is required")
		}
		if c.Database.Password == "" {
			return fmt.Errorf("database password is required")
		}
	case "sqlite":
		if c.Database.Name == "" {
			return fmt.Errorf("database name (filepath) is required for sqlite")
		}
	default:
		return fmt.Errorf("unsupported database driver: %s", c.Database.Driver)
	}
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Database.Port == 0 {
		return fmt.Errorf("database port is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database user is required")
	}
	if c.Database.Password == "" {
		return fmt.Errorf("database password is required")
	}

	return nil
}
