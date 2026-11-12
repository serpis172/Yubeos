package clock

import (
	"testing"
	"time"
)

func TestFakeAdvance(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f := NewFake(t0)
	f.Advance(90 * time.Second)
	if got := f.Now().Sub(t0); got != 90*time.Second {
		t.Fatalf("advanced by %v", got)
	}
}
