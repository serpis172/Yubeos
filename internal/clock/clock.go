// Package clock abstracts time so schedulers, sessions and rollups are testable.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current time.
type Clock interface{ Now() time.Time }

// Real is the wall clock.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

// Fake is a manually advanced clock for tests.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

func NewFake(t time.Time) *Fake { return &Fake{t: t} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}
