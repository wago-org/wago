//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestAsyncInterruptRetainsQueuedDeliveryToken(t *testing.T) {
	code, _, err := MapCode([]byte{0xc3})
	if err != nil {
		t.Fatal(err)
	}
	defer Unmap(code)
	trap := make([]byte, TrapBufferBytes)
	ptr := slicePtr(trap)
	stop := RequestInterruptAsync(trap)
	defer stop()
	var request *interruptRequest
	deadline := time.Now().Add(time.Second)
	for request == nil {
		for i := range interruptRequests {
			if atomic.LoadUintptr(&interruptRequests[i].trap) == ptr {
				request = &interruptRequests[i]
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("async owner never published")
		}
		time.Sleep(50 * time.Microsecond)
	}
	token := atomic.LoadUint64(&request.token)
	for i := 0; i < 4; i++ {
		time.Sleep(time.Millisecond)
		if atomic.LoadUintptr(&request.trap) != ptr || atomic.LoadUint64(&request.token) != token {
			t.Fatal("queued-delivery token retired while retry owner is active")
		}
	}
	stop()
	if atomic.LoadUintptr(&request.trap) != 0 {
		t.Fatal("stop retained request ownership")
	}
	next := acquireInterruptRequest(ptr)
	if next == nil {
		t.Fatal("request unavailable after stop")
	}
	defer releaseInterruptRequest(next, ptr)
	if atomic.LoadUint64(&next.token) == token {
		t.Fatal("new owner accepts retired delivery token")
	}
}
