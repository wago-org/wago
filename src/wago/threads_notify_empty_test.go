//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
	goruntime "runtime"
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
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
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
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
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
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
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

// The notifier writes with an ordinary i32.store, not an atomic store that
// could supply its own ordering. Its empty-queue check needs StoreLoad ordering.
func plainStoreNotifyModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 2, 3, 1, 1)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("publish", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0, 0x41, 1, 0x36, 2, 0, // plain i32.store(0, 1)
			0x41, 0, 0x41, 1, 0xfe, 0, 2, 0, 0x0b, // memory.atomic.notify(0, 1)
		}))),
	)
}

func TestThreadsNotifyPlainStoreOrdering(t *testing.T) {
	old := goruntime.GOMAXPROCS(2)
	defer goruntime.GOMAXPROCS(old)
	var orderedCheck []byte
	switch goruntime.GOARCH {
	case "amd64":
		a := &x64.Asm{}
		a.MovImm32(x64.RAX, 0)
		a.LockAluIdx(0x09, x64.RSP, x64.RAX, x64.RAX, 0, 4)
		a.Load32(x64.RDI, x64.RSI, -abi.SharedNotifyPendingOffset)
		orderedCheck = a.B
	case "arm64":
		a := &a64.Asm{}
		a.DmbIsh()
		a.Ldar(a64.X9, a64.X16, 4)
		orderedCheck = a.B
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).
				WithBoundsChecks(BoundsChecksExplicit).WithOptimization("atomic-notify-empty", enabled)
			c, err := cfg.Compile(plainStoreNotifyModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if got := bytes.Contains(c.code, orderedCheck); got != enabled {
				t.Fatalf("store-to-load barrier immediately before pending load = %t, optimization = %t", got, enabled)
			}
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			for artifact, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				if code != c {
					defer code.Close()
				}
				if got := bytes.Contains(code.code, orderedCheck); got != enabled {
					t.Fatalf("artifact %d lost ordered pending check", artifact)
				}
				in, err := Instantiate(code, NewImports().Memory("env", "memory", memory))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				type result struct {
					value uint32
					err   error
				}
				for round := 0; round < 1024; round++ {
					atomic.StoreUint32((*uint32)(unsafe.Pointer(&memory.jm.HostBytes()[0])), 0)
					start := make(chan struct{})
					waited := make(chan result, 1)
					go func() { <-start; value, err := memory.wait32(ctx, 0, 0, -1); waited <- result{value, err} }()
					close(start)
					values, err := in.Invoke("publish")
					if err != nil || len(values) != 1 {
						cancel()
						<-waited
						t.Fatalf("notify=%v,%v", values, err)
					}
					var w result
					select {
					case w = <-waited:
					case <-ctx.Done():
						cancel()
						<-waited
						t.Fatalf("artifact %d round %d: plain store+notify missed registering waiter", artifact, round)
					}
					if w.err != nil || !((values[0] == 1 && w.value == memoryWaitNotified) || (values[0] == 0 && w.value == memoryWaitNotEqual)) {
						t.Fatalf("artifact %d round %d notify=%d wait=%+v", artifact, round, values[0], w)
					}
				}
				cancel()
			}
		})
	}
}
