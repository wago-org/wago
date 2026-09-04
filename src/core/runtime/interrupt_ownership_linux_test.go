//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestInterruptSignalOwnership(t *testing.T) {
	if os.Getenv("WAGO_TEST_SIGNAL_OWNERSHIP") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptSignalOwnership$")
		cmd.Env = append(os.Environ(), "WAGO_TEST_SIGNAL_OWNERSHIP=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("signal ownership: %v\n%s", err, out)
		}
		return
	}
	notifications := make(chan os.Signal, 64)
	for sig := 35; sig <= 64; sig++ {
		signal.Notify(notifications, syscall.Signal(sig))
	}
	defer signal.Stop(notifications)
	var before [65]interruptSigaction
	for sig := 35; sig <= 64; sig++ {
		if err := interruptRTSigaction(uintptr(sig), nil, &before[sig]); err != nil {
			t.Fatal(err)
		}
	}
	code, _, err := MapCode([]byte{0xc3})
	if err != nil {
		t.Fatal(err)
	}
	sig := interruptSignal
	var during interruptSigaction
	if err := interruptRTSigaction(uintptr(sig), nil, &during); err != nil {
		t.Fatal(err)
	}
	if during.flags != before[sig].flags || during.mask != before[sig].mask || during.restorer != before[sig].restorer {
		t.Fatalf("action properties changed: before=%+v during=%+v", before[sig], during)
	}
	notify := func() {
		t.Helper()
		_, _, errno := syscall.RawSyscall(syscall.SYS_TGKILL, uintptr(syscall.Getpid()), uintptr(syscall.Gettid()), uintptr(sig))
		if errno != 0 {
			t.Fatal(errno)
		}
		select {
		case got := <-notifications:
			if got != syscall.Signal(sig) {
				t.Fatalf("got %v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("unowned delivery lost")
		}
	}
	notify()
	if err := Unmap(code); err != nil {
		t.Fatal(err)
	}
	var after interruptSigaction
	if err := interruptRTSigaction(uintptr(sig), nil, &after); err != nil {
		t.Fatal(err)
	}
	if after != before[sig] {
		t.Fatalf("action not fully restored: before=%+v after=%+v", before[sig], after)
	}
	notify()
	// A new install must support a new mapping after final restoration.
	code, _, err = MapCode([]byte{0xc3})
	if err != nil {
		t.Fatal(err)
	}
	if err := Unmap(code); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptTokenChangesOnTrapReuse(t *testing.T) {
	first := acquireInterruptRequest(16)
	if first == nil {
		t.Fatal("request unavailable")
	}
	token := first.token
	releaseInterruptRequest(first, 16)
	second := acquireInterruptRequest(16)
	if second == nil {
		t.Fatal("request unavailable")
	}
	defer releaseInterruptRequest(second, 16)
	if token == second.token {
		t.Fatal("reused trap retained stale timer token")
	}
}

func TestInterruptRequestPublishesTokenBeforeTrap(t *testing.T) {
	// Distinct synthetic trap addresses all map to the same slot. The observer
	// models the handler's acquire-load order without delivering any signals.
	interruptRequestMu.Lock()
	base := interruptSequence
	interruptRequestMu.Unlock()
	const iterations = 100000
	if uint64(base)+iterations > uint64(^uint32(0)) {
		t.Fatal("request sequence has insufficient space for test")
	}
	stop := make(chan struct{})
	observed := make(chan string, 1)
	ready := make(chan struct{})
	go func() {
		close(ready)
		for {
			select {
			case <-stop:
				observed <- ""
				return
			default:
			}
			request := &interruptRequests[0]
			trap := atomic.LoadUintptr(&request.trap)
			token := uint32(atomic.LoadUint64(&request.token))
			if trap != 0 && uint64(token) < uint64(base)+uint64(trap>>10) {
				observed <- "published request exposed an older token"
				return
			}
		}
	}()
	<-ready
	for i := uintptr(1); i <= iterations; i++ {
		trap := i << 10
		request := acquireInterruptRequest(trap)
		if request == nil {
			close(stop)
			<-observed
			t.Fatal("request unavailable")
		}
		releaseInterruptRequest(request, trap)
	}
	close(stop)
	if failure := <-observed; failure != "" {
		t.Fatal(failure)
	}
}

func TestInterruptRequestSharedOwnerRetainsToken(t *testing.T) {
	first := acquireInterruptRequest(16)
	if first == nil {
		t.Fatal("request unavailable")
	}
	second := acquireInterruptRequest(16)
	if second == nil {
		releaseInterruptRequest(first, 16)
		t.Fatal("shared request unavailable")
	}
	token := atomic.LoadUint64(&first.token)
	releaseInterruptRequest(first, 16)
	defer releaseInterruptRequest(second, 16)
	if first != second || atomic.LoadUintptr(&second.trap) != 16 || atomic.LoadUint64(&second.token) != token {
		t.Fatal("releasing one owner changed the live shared request")
	}
}
