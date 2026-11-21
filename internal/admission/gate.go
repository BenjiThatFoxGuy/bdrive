// Package admission bounds request-owned dependency setup without queues or sleepers.
package admission

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrBusy = errors.New("dependency unavailable or busy")

type entry struct {
	active         int
	failures       uint
	until, touched time.Time
}

type Gate struct {
	mu                       sync.Mutex
	entries                  map[string]*entry
	capacity, perKey, active int
	now                      func() time.Time
}

func New(capacity, perKey int, now func() time.Time) *Gate {
	return &Gate{entries: make(map[string]*entry), capacity: capacity, perKey: perKey, now: now}
}

// Cooldown records a late read failure after setup admission was released.
// The same bounded table is used, so failed streams cannot grow a second map.
func (g *Gate) Cooldown(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	e := g.entries[key]
	if e == nil {
		for k, v := range g.entries {
			if v.active == 0 && now.Sub(v.touched) > 5*time.Minute {
				delete(g.entries, k)
			}
		}
		if len(g.entries) >= g.capacity {
			return
		}
		e = &entry{}
		g.entries[key] = e
	}
	if e.failures < 5 {
		e.failures++
	}
	e.touched = now
	e.until = now.Add(min(2*time.Second*time.Duration(1<<(e.failures-1)), 30*time.Second))
}

// Begin never waits. Entries, in-flight setup, and outage probes are bounded.
// finish(true) applies exponential cooldown (2s to 30s), with no automatic retries.
func (g *Gate) Begin(ctx context.Context, key string) (func(bool), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	e := g.entries[key]
	if e == nil {
		for k, v := range g.entries {
			if v.active == 0 && now.Sub(v.touched) > 5*time.Minute {
				delete(g.entries, k)
			}
		}
		if len(g.entries) >= g.capacity {
			return nil, ErrBusy
		}
		e = &entry{}
		g.entries[key] = e
	}
	if g.active >= 16 || e.active >= g.perKey || now.Before(e.until) {
		return nil, ErrBusy
	}
	e.active++
	g.active++
	e.touched = now
	generation := e.failures
	var once sync.Once
	return func(failed bool) {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			e.active--
			g.active--
			e.touched = g.now()
			if failed {
				if e.failures < 5 {
					e.failures++
				}
				delay := min(2*time.Second*time.Duration(1<<(e.failures-1)), 30*time.Second)
				e.until = g.now().Add(delay)
			} else if e.failures == generation && !g.now().Before(e.until) {
				e.failures = 0
				if e.active == 0 {
					delete(g.entries, key)
				}
			}
		})
	}, nil
}
