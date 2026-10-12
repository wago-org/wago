//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestThreadsNotifyQueuePublication(t *testing.T) {
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := memory.wait32(ctx, 0, 0, -1); done <- err }()
	defer func() { cancel(); <-done }()
	state := memory.state.Load()
	for {
		state.mu.Lock()
		ws := state.waiterStateLocked(false)
		queued := ws != nil && ws.active != 0
		if queued {
			if got := memory.jm.SharedNotifyPending(); got != 1 {
				state.mu.Unlock()
				t.Fatalf("shared backing does not publish its live waiter: cell=%d", got)
			}
			state.mu.Unlock()
			break
		}
		state.mu.Unlock()
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	for _, expected := range []uint32{0, 1} {
		if _, err := memory.wait32(ctx, 8, expected, 0); err != nil {
			t.Fatal(err)
		}
		if got := memory.jm.SharedNotifyPending(); got != 1 {
			t.Fatalf("nonblocking wait hid the existing queue: %d", got)
		}
	}
	if got, err := memory.notify(0, 1); err != nil || got != 1 {
		t.Fatalf("notify=%d,%v", got, err)
	}
	state.mu.Lock()
	got := memory.jm.SharedNotifyPending()
	state.mu.Unlock()
	if got != 0 {
		t.Fatalf("notified queue remains published: %d", got)
	}
}

func TestThreadsNotifyPublishesBeforeWaitRegistration(t *testing.T) {
	memory, err := NewSharedMemory(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).
		WithBoundsChecks(BoundsChecksExplicit).
		Compile(threadsSecondaryAtomicModuleWithSharing(false, false, true, false, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, NewImports().Memory("env", "memory", memory).Memory("env", "secondary", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		value uint32
		err   error
	}
	waited := make(chan result, 1)
	notified := make(chan result, 1)
	// Block queue allocation after the value comparison. Publishing only when
	// inserting the queue would let native notify bypass this pending wait.
	memoryWaiters.Lock()
	go func() {
		value, err := memory.wait32(ctx, 8, 0, -1)
		waited <- result{value, err}
	}()
	for memory.jm.SharedNotifyPending() == 0 {
		select {
		case <-ctx.Done():
			memoryWaiters.Unlock()
			<-waited
			t.Fatal("wait did not publish before accessing the queue registry")
		case <-time.After(time.Millisecond):
		}
	}
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&memory.jm.HostBytes()[8])), 1)
	go func() {
		values, err := in.Invoke("helper0", 0, 1)
		var value uint32
		if len(values) != 0 {
			value = uint32(values[0])
		}
		notified <- result{value, err}
	}()
	memoryWaiters.Unlock()
	n, w := <-notified, <-waited
	if n.err != nil || w.err != nil {
		t.Fatalf("notify=%+v wait=%+v", n, w)
	}
	if !((n.value == 1 && w.value == memoryWaitNotified) || (n.value == 0 && w.value == memoryWaitNotEqual)) {
		t.Fatalf("store/notify missed a registering wait: notify=%d wait=%d", n.value, w.value)
	}
	if got := memory.jm.SharedNotifyPending(); got != 0 {
		t.Fatalf("completed registration remains published: %d", got)
	}
}

func TestThreadsNotifyEmptyPolicyCodecAndTraps(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("enabled=%t/wide=%t/shared=%t/regabi=%t", enabled, wide, shared, regABI), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("atomic-notify-empty", enabled).WithOptimization("reg-abi", regABI)
						c, err := cfg.Compile(threadsSecondaryAtomicModuleWithSharing(!wide, wide, false, false, shared))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						if c.requiresNotifyPending != enabled {
							t.Fatalf("runtime requirement=%t", c.requiresNotifyPending)
						}
						for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
							if compiled.requiresNotifyPending != enabled {
								t.Fatal("codec lost notify ABI requirement")
							}
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							for _, count := range []uint64{0, 1, ^uint64(0)} {
								got, err := in.Invoke("helper0", 0, count)
								if err != nil || len(got) != 1 || got[0] != 0 {
									t.Fatalf("empty count=%x: %v,%v", count, got, err)
								}
							}
							for _, addr := range []uint64{1, 65536, 1 << 32, ^uint64(0) - 3} {
								if !wide && addr == 1<<32 {
									got, err := in.Invoke("helper0", addr, 1)
									if err != nil || got[0] != 0 {
										t.Fatalf("i32 carrier=%v,%v", got, err)
									}
									continue
								}
								_, err := in.Invoke("helper0", addr, 1)
								var trap *TrapError
								want := TrapLinMemOutOfBounds
								if addr == 1 {
									want = TrapAtomicUnaligned
								}
								// memory32 truncates the carrier but retains the eight-byte memarg.
								if !errors.As(err, &trap) || trap.Code != want {
									t.Fatalf("address=%x: %v, want %v", addr, err, want)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestThreadsNotifyPendingQueuesUseSlowPath(t *testing.T) {
	for _, regABI := range []bool{false, true} {
		t.Run(fmt.Sprint(regABI), func(t *testing.T) {
			memory, err := NewSharedMemory(1, 3)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
			c, err := cfg.Compile(threadsSecondaryAtomicModuleWithSharing(false, false, true, false, true))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				in, err := Instantiate(compiled, NewImports().Memory("env", "memory", memory).Memory("env", "secondary", memory))
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				done := make(chan error, 2)
				for _, offset := range []uint64{8, 16} {
					go func(offset uint64) {
						got, err := memory.wait32(ctx, offset, 0, -1)
						if err == nil && got != 0 {
							err = fmt.Errorf("wait result=%d", got)
						}
						done <- err
					}(offset)
				}
				state := memory.state.Load()
				for {
					state.mu.Lock()
					ws := state.waiterStateLocked(false)
					ready := ws != nil && ws.active == 2
					state.mu.Unlock()
					if ready {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
				for i, address := range []uint64{0, 8} {
					got, err := in.Invoke("helper0", address, 1)
					if err != nil || got[0] != 1 {
						t.Fatalf("queued notify=%v,%v", got, err)
					}
					want := uint32(1)
					if i == 1 {
						want = 0
					}
					if got := memory.jm.SharedNotifyPending(); got != want {
						t.Fatalf("queue publication=%d want %d", got, want)
					}
				}
				for i := 0; i < 2; i++ {
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
				cancel()
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
