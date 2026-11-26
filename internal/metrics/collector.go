package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Provider is one hardware/OS source. Sample must be cheap (it runs every Interval) and compute rates from the
// real time elapsed since its previous call, never from the nominal interval.
type Provider interface {
	Name() string
	Interval() time.Duration
	// Devices lists the hardware currently present (re-read periodically; may be empty).
	Devices(ctx context.Context) []Device
	// Sample returns one reading per series; an error marks the provider unavailable until the next success.
	Sample(ctx context.Context, now time.Time) ([]Sample, error)
}

// Starter is implemented by providers that own a background process (nvidia-smi).
type Starter interface{ Start(ctx context.Context) }

// Collector drives the providers: one goroutine each, drift-free tickers, status tracking, device refresh.
type Collector struct {
	store     *Store
	providers []Provider
	log       *slog.Logger
	now       func() time.Time
	devEvery  time.Duration
}

// NewCollector wires providers to a store.
func NewCollector(store *Store, log *slog.Logger, providers ...Provider) *Collector {
	return &Collector{store: store, providers: providers, log: log, now: time.Now, devEvery: 30 * time.Second}
}

// Run blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range c.providers {
		if st, ok := p.(Starter); ok {
			st.Start(ctx)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.loop(ctx, p)
		}()
	}
	wg.Wait()
}

// Tick performs one round of one provider immediately (tests and priming).
func (c *Collector) Tick(ctx context.Context, p Provider) { c.collect(ctx, p, c.now()) }

func (c *Collector) loop(ctx context.Context, p Provider) {
	c.store.SetDevices(p.Name(), p.Devices(ctx))
	c.collect(ctx, p, c.now()) // first call primes the counters of delta-based providers
	t := time.NewTicker(p.Interval())
	defer t.Stop()
	dev := time.NewTicker(c.devEvery)
	defer dev.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-dev.C:
			c.store.SetDevices(p.Name(), p.Devices(ctx))
		case now := <-t.C:
			c.collect(ctx, p, now)
		}
	}
}

func (c *Collector) collect(ctx context.Context, p Provider, now time.Time) {
	samples, err := p.Sample(ctx, now)
	if err != nil {
		if prev, ok := c.status(p.Name()); !ok || prev.OK || prev.Reason != err.Error() {
			c.log.Warn("metrics provider unavailable", "provider", p.Name(), "err", err)
		}
		c.store.SetStatus(ProviderStatus{Name: p.Name(), OK: false, Reason: err.Error()})
		return
	}
	if prev, ok := c.status(p.Name()); ok && !prev.OK {
		c.log.Info("metrics provider recovered", "provider", p.Name())
	}
	c.store.SetStatus(ProviderStatus{Name: p.Name(), OK: true})
	c.store.Put(now, samples)
}

func (c *Collector) status(name string) (ProviderStatus, bool) {
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()
	st, ok := c.store.status[name]
	return st, ok
}
