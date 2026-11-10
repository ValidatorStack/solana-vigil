// Package metrics exposes a low-cardinality Prometheus view of monitor snapshots.
package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

type Collector struct {
	monitor  *monitor.Monitor
	ttl      time.Duration
	mu       sync.Mutex
	cached   monitor.Snapshot
	cachedAt time.Time
	flight   chan struct{}
}

func NewCollector(source *monitor.Monitor, cacheTTL time.Duration) *Collector {
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Second
	}
	return &Collector{monitor: source, ttl: cacheTTL}
}

func (c *Collector) Snapshot(ctx context.Context) (monitor.Snapshot, error) {
	for {
		c.mu.Lock()
		if !c.cachedAt.IsZero() && time.Since(c.cachedAt) < c.ttl {
			snapshot := c.cached
			c.mu.Unlock()
			return snapshot, nil
		}
		if c.flight != nil {
			flight := c.flight
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				c.mu.Lock()
				snapshot := c.cached
				c.mu.Unlock()
				return snapshot, ctx.Err()
			case <-flight:
			}
			continue
		}
		flight := make(chan struct{})
		c.flight = flight
		c.mu.Unlock()

		snapshot := c.monitor.Collect(ctx)
		err := ctx.Err()
		c.mu.Lock()
		if err == nil {
			c.cached = snapshot
			c.cachedAt = time.Now()
		}
		c.flight = nil
		close(flight)
		cached := c.cached
		c.mu.Unlock()
		if err != nil {
			return cached, err
		}
		return snapshot, nil
	}
}

func (c *Collector) Ready(maxAge time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.cachedAt.IsZero() && time.Since(c.cachedAt) <= maxAge
}
