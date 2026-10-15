package provider

import (
	"context"
	"testing"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
)

type stubProvider struct {
	*BaseProvider
}

func (p *stubProvider) FetchEPG(context.Context, string, string, time.Time) ([]*model.Program, error) {
	return nil, nil
}

func (p *stubProvider) FetchEPGBatch(context.Context, []*model.ChannelMappingInfo, time.Time) ([]*model.Program, error) {
	return nil, nil
}

func (p *stubProvider) HealthCheck(context.Context) *model.ProviderHealth {
	return &model.ProviderHealth{Healthy: true}
}

func newStub(config *model.ProviderConfig) (Provider, error) {
	return &stubProvider{BaseProvider: NewBaseProvider(config, nil)}, nil
}

func newTestFactory() *Factory {
	r := NewRegistry()
	r.Register(Definition{ID: "a", BaseURL: "https://a", Priority: 2, Timeout: 10 * time.Second, RateLimit: 5, MaxRetries: 2, New: newStub})
	r.Register(Definition{ID: "b", Name: "B", BaseURL: "https://b", Priority: 1, RateLimit: 10, New: newStub})
	return &Factory{registry: r}
}

func ptr[T any](v T) *T { return &v }

func TestResolveConfigsDefaults(t *testing.T) {
	configs := newTestFactory().ResolveConfigs(nil)
	if len(configs) != 2 {
		t.Fatalf("got %d configs, want 2", len(configs))
	}
	a := configs[0]
	if a.ID != "a" || a.Name != "a" || !a.Enabled || a.BaseURL != "https://a" || a.Priority != 2 || a.Timeout != 10*time.Second || a.RateLimit != 5 {
		t.Errorf("unexpected default config: %+v", a)
	}
	if configs[1].Name != "B" {
		t.Errorf("Name = %q, want B", configs[1].Name)
	}
}

func TestResolveConfigsOverride(t *testing.T) {
	configs := newTestFactory().ResolveConfigs(model.ProviderOverrides{
		"a":       {Priority: ptr(9), Timeout: ptr(3 * time.Second)},
		"b":       {Enabled: ptr(false)},
		"unknown": {Enabled: ptr(false)},
	})

	a := configs[0]
	if a.Priority != 9 || a.Timeout != 3*time.Second {
		t.Errorf("override not applied: %+v", a)
	}
	if a.BaseURL != "https://a" || a.RateLimit != 5 || a.MaxRetries != 2 || !a.Enabled {
		t.Errorf("non-overridden fields changed: %+v", a)
	}
	if configs[1].Enabled {
		t.Error("b should be disabled")
	}
}

func TestCreateProvidersSkipsDisabled(t *testing.T) {
	providers := newTestFactory().CreateProviders(model.ProviderOverrides{
		"b": {Enabled: ptr(false)},
	})
	if len(providers) != 1 || providers[0].GetID() != "a" {
		t.Fatalf("got %d providers, want only a", len(providers))
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	r := NewRegistry()
	r.Register(Definition{ID: "a", New: newStub})
	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate id")
		}
	}()
	r.Register(Definition{ID: "a", New: newStub})
}
