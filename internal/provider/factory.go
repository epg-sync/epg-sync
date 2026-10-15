package provider

import (
	"maps"
	"slices"

	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/logger"
)

type Factory struct {
	registry *Registry
}

func NewFactory() *Factory {
	return &Factory{
		registry: GlobalRegistry(),
	}
}

// ResolveConfigs 将所有已注册 provider 的默认配置与 config.yaml 中的覆盖项合并
func (f *Factory) ResolveConfigs(overrides model.ProviderOverrides) []model.ProviderConfig {
	defs := f.registry.Definitions()
	configs := make([]model.ProviderConfig, 0, len(defs))
	known := make(map[string]bool, len(defs))

	for _, def := range defs {
		known[def.ID] = true
		configs = append(configs, mergeConfig(def, overrides[def.ID]))
	}

	for _, id := range slices.Sorted(maps.Keys(overrides)) {
		if !known[id] {
			logger.Warn("Unknown provider id in config, ignored", logger.String("id", id))
		}
	}

	return configs
}

func (f *Factory) CreateProviders(overrides model.ProviderOverrides) []Provider {
	var (
		providers []Provider
		enabled   []string
		disabled  []string
	)

	for _, cfg := range f.ResolveConfigs(overrides) {
		if !cfg.Enabled {
			disabled = append(disabled, cfg.ID)
			continue
		}

		p, err := f.registry.Create(&cfg)
		if err != nil {
			logger.Warn("Failed to create provider", logger.String("id", cfg.ID), logger.Err(err))
			continue
		}

		providers = append(providers, p)
		enabled = append(enabled, cfg.ID)
	}

	logger.Info("Providers loaded",
		logger.Strings("enabled", enabled),
		logger.Strings("disabled", disabled),
	)

	return providers
}

func (f *Factory) CreateChain(overrides model.ProviderOverrides, cache cache.Cache) *Chain {
	return NewChain(cache, f.CreateProviders(overrides)...)
}

func mergeConfig(def Definition, o model.ProviderOverride) model.ProviderConfig {
	cfg := model.ProviderConfig{
		ID:         def.ID,
		Name:       def.Name,
		BaseURL:    def.BaseURL,
		Enabled:    true,
		Priority:   def.Priority,
		Timeout:    def.Timeout,
		RateLimit:  def.RateLimit,
		MaxRetries: def.MaxRetries,
		Settings:   maps.Clone(def.Settings),
	}

	if o.Enabled != nil {
		cfg.Enabled = *o.Enabled
	}
	if o.Name != nil {
		cfg.Name = *o.Name
	}
	if o.BaseURL != nil {
		cfg.BaseURL = *o.BaseURL
	}
	if o.Priority != nil {
		cfg.Priority = *o.Priority
	}
	if o.Timeout != nil {
		cfg.Timeout = *o.Timeout
	}
	if o.RateLimit != nil {
		cfg.RateLimit = *o.RateLimit
	}
	if o.MaxRetries != nil {
		cfg.MaxRetries = *o.MaxRetries
	}
	if len(o.Settings) > 0 {
		if cfg.Settings == nil {
			cfg.Settings = make(map[string]any, len(o.Settings))
		}
		maps.Copy(cfg.Settings, o.Settings)
	}

	return cfg
}

var globalFactory = NewFactory()

func GlobalFactory() *Factory {
	return globalFactory
}
