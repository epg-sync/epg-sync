package model

import (
	"fmt"
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
	*o = m
	return nil
}

type ProviderHealth struct {
	ProviderID string    `json:"provider_id"`
	Healthy    bool      `json:"healthy"`
	Message    string    `json:"message"`
	CheckedAt  time.Time `json:"checked_at"`
	Latency    int64     `json:"latency"`
	StatusCode int       `json:"status_code"`
}
