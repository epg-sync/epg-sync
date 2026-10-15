package provider

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/errors"
)

type ProviderFactory func(config *model.ProviderConfig) (Provider, error)

// Definition 是 provider 在代码里声明的默认配置，config.yaml 只需按 id 覆盖
type Definition struct {
	ID         string
	Name       string
	BaseURL    string
	Priority   int
	Timeout    time.Duration
	RateLimit  int
	MaxRetries int
	Settings   map[string]any
	New        ProviderFactory
}

type Registry struct {
	mu          sync.RWMutex
	definitions map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{
		definitions: make(map[string]Definition),
	}
}

// Register 在 init 中调用，id 重复或缺少构造函数属于编码错误，直接 panic
func (r *Registry) Register(def Definition) {
	if def.ID == "" {
		panic("provider: Register called with empty id")
	}
	if def.New == nil {
		panic(fmt.Sprintf("provider: Register %q called with nil New", def.ID))
	}
	if def.Name == "" {
		def.Name = def.ID
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.definitions[def.ID]; exists {
		panic(fmt.Sprintf("provider: Register called twice for %q", def.ID))
	}
	r.definitions[def.ID] = def
}

// Definitions 按 id 排序返回所有已注册的 provider
func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]Definition, 0, len(r.definitions))
	for _, def := range r.definitions {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].ID < defs[j].ID
	})
	return defs
}

func (r *Registry) Create(config *model.ProviderConfig) (Provider, error) {
	r.mu.RLock()
	def, exists := r.definitions[config.ID]
	r.mu.RUnlock()

	if !exists {
		return nil, errors.New(
			errors.ErrCodeProviderNotFound,
			fmt.Sprintf("provider type not registered: %s", config.ID),
		)
	}

	return def.New(config)
}

var globalRegistry = NewRegistry()

func GlobalRegistry() *Registry {
	return globalRegistry
}

func Register(def Definition) {
	globalRegistry.Register(def)
}

func Definitions() []Definition {
	return globalRegistry.Definitions()
}
