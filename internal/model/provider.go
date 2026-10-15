package model

import (
	"fmt"
	"maps"
	"time"
)

type ProviderConfig struct {
	ID         string         `yaml:"id"`
	Name       string         `yaml:"name"`
	BaseURL    string         `yaml:"base_url"`
	Enabled    bool           `yaml:"enabled"`
	Priority   int            `yaml:"priority"`
	Timeout    time.Duration  `yaml:"timeout"`
	RateLimit  int            `yaml:"rate_limit"`
	MaxRetries int            `yaml:"max_retries"`
	Settings   map[string]any `yaml:"settings"`
}

// ProviderOverride 是 config.yaml 中对单个 provider 的覆盖项，未填写的字段沿用代码中的默认值
type ProviderOverride struct {
	Enabled    *bool          `yaml:"enabled"`
	Name       *string        `yaml:"name"`
	BaseURL    *string        `yaml:"base_url"`
	Priority   *int           `yaml:"priority"`
	Timeout    *time.Duration `yaml:"timeout"`
	RateLimit  *int           `yaml:"rate_limit"`
	MaxRetries *int           `yaml:"max_retries"`
	Settings   map[string]any `yaml:"settings"`
}

// ProviderOverrides 以 provider id 为 key
type ProviderOverrides map[string]ProviderOverride

func (o *ProviderOverrides) UnmarshalYAML(unmarshal func(any) error) error {
	var list []any
	if err := unmarshal(&list); err == nil {
		if len(list) == 0 {
			*o = ProviderOverrides{}
			return nil
		}
		return fmt.Errorf("providers 已改为以 provider id 为 key 的写法（例如 providers: {ysp: {enabled: false}}），不再支持列表写法，参见 INSTALL.md")
	}

	var m map[string]ProviderOverride
	if err := unmarshal(&m); err != nil {
		return err
	}

	// 合并到已有的覆盖项上，后加载的配置文件只改动它写到的字段
	if *o == nil {
		*o = make(ProviderOverrides, len(m))
	}
	for id, override := range m {
		(*o)[id] = (*o)[id].merge(override)
	}
	return nil
}

// merge 返回以 o 为基础、用 n 中已填写字段覆盖后的结果
func (o ProviderOverride) merge(n ProviderOverride) ProviderOverride {
	if n.Enabled != nil {
		o.Enabled = n.Enabled
	}
	if n.Name != nil {
		o.Name = n.Name
	}
	if n.BaseURL != nil {
		o.BaseURL = n.BaseURL
	}
	if n.Priority != nil {
		o.Priority = n.Priority
	}
	if n.Timeout != nil {
		o.Timeout = n.Timeout
	}
	if n.RateLimit != nil {
		o.RateLimit = n.RateLimit
	}
	if n.MaxRetries != nil {
		o.MaxRetries = n.MaxRetries
	}
	if len(n.Settings) > 0 {
		settings := make(map[string]any, len(o.Settings)+len(n.Settings))
		maps.Copy(settings, o.Settings)
		maps.Copy(settings, n.Settings)
		o.Settings = settings
	}
	return o
}

type ProviderHealth struct {
	ProviderID string    `json:"provider_id"`
	Healthy    bool      `json:"healthy"`
	Message    string    `json:"message"`
	CheckedAt  time.Time `json:"checked_at"`
	Latency    int64     `json:"latency"`
	StatusCode int       `json:"status_code"`
}
