package delegation

import (
	"context"
	"sync"
	"time"
)

// Capacity shares admission across workflows using the same provider binding.
// Background work leaves room for interactive calls. A rate-limit response
// reduces admission temporarily; it never replays the refused request.
type Capacity struct {
	mu      sync.Mutex
	entries map[string]*capacityEntry
	changed chan struct{}
}
type capacityEntry struct {
	active, background int
	slowUntil          time.Time
}

func (c *Capacity) take(key string, background bool) (func(), <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*capacityEntry{}
		c.changed = make(chan struct{})
	}
	e := c.entries[key]
	if e == nil {
		e = &capacityEntry{}
		c.entries[key] = e
	}
	limit := 4
	if time.Now().Before(e.slowUntil) {
		limit = 1
	}
	if e.active >= limit || background && e.background >= 2 {
		return nil, c.changed
	}
	e.active++
	if background {
		e.background++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			e.active--
			if background {
				e.background--
			}
			if e.active == 0 && !time.Now().Before(e.slowUntil) {
				delete(c.entries, key)
			}
			close(c.changed)
			c.changed = make(chan struct{})
		})
	}, nil
}
func (c *Capacity) Try(key string) (func(), bool) {
	release, _ := c.take(key, false)
	return release, release != nil
}
func (c *Capacity) Acquire(ctx context.Context, key string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, changed := c.take(key, true)
		if release != nil {
			return release, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		case <-time.After(time.Second):
		}
	}
}
func (c *Capacity) Throttle(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*capacityEntry{}
		c.changed = make(chan struct{})
	}
	e := c.entries[key]
	if e == nil {
		e = &capacityEntry{}
		c.entries[key] = e
	}
	e.slowUntil = time.Now().Add(30 * time.Second)
}
