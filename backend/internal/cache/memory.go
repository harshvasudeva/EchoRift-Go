package cache

import (
	"context"
	"sync"
	"time"
)

type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
	now     func() time.Time
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		entries: make(map[string]memoryEntry),
		now:     time.Now,
	}
}

func (c *MemoryCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if !entry.expiresAt.IsZero() && c.now().After(entry.expiresAt) {
		_ = c.Delete(ctx, key)
		return nil, false, nil
	}
	value := append([]byte(nil), entry.value...)
	return value, true, nil
}

func (c *MemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = c.now().Add(ttl)
	}

	c.mu.Lock()
	c.entries[key] = memoryEntry{value: append([]byte(nil), value...), expiresAt: expiresAt}
	c.mu.Unlock()
	return nil
}

func (c *MemoryCache) Delete(ctx context.Context, key string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
	return nil
}

func (c *MemoryCache) TTL(ctx context.Context, key string) (time.Duration, bool, error) {
	select {
	case <-ctx.Done():
		return 0, false, ctx.Err()
	default:
	}

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return 0, false, nil
	}
	if entry.expiresAt.IsZero() {
		return 0, true, nil
	}
	ttl := time.Until(entry.expiresAt)
	if ttl <= 0 {
		_ = c.Delete(ctx, key)
		return 0, false, nil
	}
	return ttl, true, nil
}
