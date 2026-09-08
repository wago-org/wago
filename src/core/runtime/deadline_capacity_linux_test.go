//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"errors"
	"testing"
	"time"
)

func TestDeadlineCapacityAdmission(t *testing.T) {
	// Reserve all slots without running guest code or starting kernel timers.
	var held []*interruptRequest
	for i := 0; i < maxInterruptRequests; i++ {
		r := acquireInterruptRequest(uintptr(i+1) * 16)
		if r == nil {
			t.Fatalf("slot %d unavailable", i)
		}
		held = append(held, r)
	}
	defer func() {
		for i, r := range held {
			releaseInterruptRequest(r, uintptr(i+1)*16)
		}
	}()
	trap := make([]byte, TrapBufferBytes)
	stop, err := SetInterruptDeadline(trap, time.Now().Add(time.Hour))
	if stop != nil {
		stop()
		t.Fatal("deadline admitted beyond capacity")
	}
	var limit *ResourceLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("error = %v, want resource limit", err)
	}
}

func TestInterruptRequestSharedOwnership(t *testing.T) {
	a := acquireInterruptRequest(16)
	b := acquireInterruptRequest(16)
	if a == nil || a != b {
		t.Fatal("same trap did not share slot")
	}
	releaseInterruptRequest(a, 16)
	if b.trap != 16 || b.refs != 1 {
		t.Fatalf("live owner was cleared: %+v", b)
	}
	releaseInterruptRequest(b, 16)
	if b.trap != 0 || b.refs != 0 {
		t.Fatalf("slot not released: %+v", b)
	}
}
