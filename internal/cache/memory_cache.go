package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/patrickmn/go-cache"
)

type MemoryCache struct {
	c *cache.Cache
}

func NewMemoryCache() Cache {
	c := cache.New(5*time.Minute, 10*time.Minute)
	return &MemoryCache{c: c}
}

func (m *MemoryCache) Get(ctx context.Context, key string, dest any) error {
	val, found := m.c.Get(key)
	if !found {
		return errors.CacheMiss(key)
	}

	data, ok := val.([]byte)
	if !ok {
		return errors.Wrap(nil, errors.ErrCodeCacheInvalid, "invalid cache data type")
	}

	err := json.Unmarshal(data, dest)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeCacheInvalid, "failed to unmarshal value")
	}

	return nil
}

func (m *MemoryCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeCacheInvalid, "failed to marshal value")
	}

	m.c.Set(key, data, ttl)
	return nil
}

func (m *MemoryCache) Delete(ctx context.Context, keys ...string) error {
	for _, k := range keys {
		m.c.Delete(k)
	}
	return nil
}

func (m *MemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	_, found := m.c.Get(key)
	return found, nil
}

func (m *MemoryCache) Ping(ctx context.Context) error {
	return nil
}

func (m *MemoryCache) Close() error {
	return nil
}
