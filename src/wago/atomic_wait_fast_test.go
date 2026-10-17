//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestAtomicWaitFastReturnsAndTraps(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, sub := range []byte{0, 1, 2} {
		for _, zero := range []bool{false, true} {
			for _, off := range []uint32{0, 7, 0x80000000, 0xfffffff8, 0xffffffff} {
				t.Run(fmt.Sprintf("op%d/zero%t/offset%x", sub, zero, off), func(t *testing.T) {
					c, err := Compile(config, atomicWaitProbeModule(sub, zero, off))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					m, err := NewSharedMemory(1, 1)
					if err != nil {
						t.Fatal(err)
					}
					defer m.Close()
					in, err := Instantiate(c, testImports("env.memory", m))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					size := uint64(4)
					if sub == 2 {
						size = 8
					}
					for _, addr := range []uint32{0, 1, 4, 8, 65528, 65532, 65536, 65537, 0x80000000, 0xffffffff} {
						for _, expected := range []uint64{0, 1, 1 << 40} {
							for _, timeout := range []uint64{0, 1} {
								effective := uint64(addr) + uint64(off)
								var wantTrap TrapCode
								if effective&(size-1) != 0 {
									wantTrap = TrapAtomicUnaligned
								} else if effective+size > 65536 {
									wantTrap = TrapLinMemOutOfBounds
								}
								out, err := in.Invoke("run", uint64(addr), expected, timeout)
								if wantTrap != 0 {
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != wantTrap {
										t.Fatalf("addr=%x value=%x timeout=%d: %v, want trap %v", addr, expected, timeout, err, wantTrap)
									}
									continue
								}
								want := uint64(0)
								if sub != 0 {
									if sub == 1 {
										expected = uint64(uint32(expected))
									}
									want = 2
									if expected != 0 {
										want = 1
									}
								}
								if err != nil || len(out) != 1 || out[0] != want {
									t.Fatalf("addr=%x value=%x timeout=%d: %v, %v; want %d", addr, expected, timeout, out, err, want)
								}
								if m.jm.SharedNotifyPending() != 0 {
									t.Fatal("retained pending waiter state")
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestAtomicWaitFastPreservesLiveValues(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, sub := range []byte{0, 1, 2} {
		for _, zero := range []bool{false, true} {
			t.Run(fmt.Sprintf("op%d/zero%t", sub, zero), func(t *testing.T) {
				align := byte(2)
				if sub == 2 {
					align = 3
				}
				body := []byte{
					0x20, 0x01, 0x42, 0x01, 0x7c, 0x21, 0x03, // dirty local = expected + 1
					0x23, 0x00, 0x42, 0x01, 0x7c, 0x24, 0x00, // dirty global++
					0x42, 0x2a, // live i64 prefix
					0x20, 0x00, // address
				}
				if sub == 0 && zero {
					body = append(body, 0x41, 0x00)
				} else {
					body = append(body, 0x20, 0x01)
					if sub != 2 {
						body = append(body, 0xa7)
					}
				}
				if sub != 0 {
					if zero {
						body = append(body, 0x42, 0x00)
					} else {
						body = append(body, 0x20, 0x02)
					}
				}
				body = append(body, 0xfe, sub, align, 0x00,
					0x20, 0x03, 0x23, 0x00, 0x85, // local xor global
					0x0b)
				code := append([]byte{0x01, 0x01, 0x7e}, body...)
				code = append(wasmtest.ULEB(uint32(len(code))), code...)
				memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
				memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I32, wasm.I64}))),
					wasmtest.Section(2, wasmtest.Vec(memoryImport)),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(6, wasmtest.Vec([]byte{0x7e, 0x01, 0x42, 0x05, 0x0b})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(code)),
				)
				c, err := Compile(config, module)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				m, err := NewSharedMemory(1, 1)
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				in, err := Instantiate(c, testImports("env.memory", m))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for i, value := range []uint64{1 << 40, 0, 0, 1} {
					// A conservative pending flag exercises the Go notify path.
					if i == 3 {
						m.jm.SetSharedNotifyPending(true)
					}
					out, err := in.Invoke("run", 0, value, 1)
					if i == 3 {
						m.jm.SetSharedNotifyPending(false)
					}
					want := uint64(0)
					if sub != 0 {
						want = 2
						compare := value
						if sub == 1 {
							compare = uint64(uint32(compare))
						}
						if compare != 0 {
							want = 1
						}
					}
					if err != nil || len(out) != 3 || out[0] != 42 || out[1] != want || out[2] != (value+1)^uint64(6+i) {
						t.Fatalf("call %d: %v, %v; want [42 %d %d]", i, out, err, want, (value+1)^uint64(6+i))
					}
				}
			})
		}
	}
}

func TestAtomicWaitFastNotifyWakesQueuedWaiter(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	c, err := Compile(config, atomicWaitProbeModule(0, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	m, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	in, err := Instantiate(c, testImports("env.memory", m))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.wait32(ctx, 0, 0, -1); done <- err }()
	waitForMemoryWaiters(t, m, 1)
	// Neither dynamic nor literal zero-count notify may consume a queued waiter.
	for _, zero := range []bool{false, true} {
		zc, err := Compile(config, atomicWaitProbeModule(0, zero, 0))
		if err != nil {
			t.Fatal(err)
		}
		defer zc.Close()
		zi, err := Instantiate(zc, testImports("env.memory", m))
		if err != nil {
			t.Fatal(err)
		}
		defer zi.Close()
		out, err := zi.Invoke("run", 0, 0, 0)
		if err != nil || len(out) != 1 || out[0] != 0 {
			t.Fatalf("zero-count notify (literal=%t) = %v, %v", zero, out, err)
		}
		if m.jm.SharedNotifyPending() != 1 {
			t.Fatal("zero-count notify consumed pending waiter state")
		}
	}
	out, err := in.Invoke("run", 0, 1, 0)
	if err != nil || len(out) != 1 || out[0] != 1 {
		t.Fatalf("notify = %v, %v; want one wake", out, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending waiter was not notified")
	}
	if m.jm.SharedNotifyPending() != 0 {
		t.Fatal("retained pending state")
	}
}

// A store followed by notify must not miss a waiter that is between
// announcing its presence and comparing the expected value.
func TestAtomicWaitFastNotifyPublicationRace(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	wc, err := Compile(config, atomicWaitProbeModule(1, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer wc.Close()
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x41, 0x01, 0xfe, 0x17, 0x02, 0x00, // atomic store(address=0, value=1)
			0x41, 0x00, 0x41, 0x01, 0xfe, 0x00, 0x02, 0x00, 0x0b,
		}))),
	)
	nc, err := Compile(config, module)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	m, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waiter, err := Instantiate(wc, testImports("env.memory", m))
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	notifier, err := Instantiate(nc, testImports("env.memory", m))
	if err != nil {
		t.Fatal(err)
	}
	defer notifier.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cell := (*uint32)(unsafe.Pointer(&m.UnsafeBytes()[0]))
	type result struct {
		value uint64
		err   error
	}
	done := make(chan result, 1)
	for i := 0; i < 2000; i++ {
		atomic.StoreUint32(cell, 0)
		go func() {
			out, err := waiter.InvokeContext(ctx, "run", 0, 0, ^uint64(0))
			var value uint64
			if len(out) != 0 {
				value = out[0]
			}
			done <- result{value, err}
		}()
		if i&1 == 0 {
			runtime.Gosched()
		}
		out, err := notifier.Invoke("run")
		if err != nil {
			t.Fatal(err)
		}
		got := <-done
		if got.err != nil || got.value+out[0] != 1 {
			t.Fatalf("iteration %d: wait=%+v notify=%v", i, got, out)
		}
		if m.jm.SharedNotifyPending() != 0 {
			t.Fatalf("iteration %d: retained pending waiter state", i)
		}
	}
}

func TestAtomicWaitFastArtifactRoundTrip(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, sub := range []byte{0, 1, 2} {
		for _, zero := range []bool{false, true} {
			t.Run(fmt.Sprintf("op%d/zero%t", sub, zero), func(t *testing.T) {
				c, err := cfg.Compile(atomicWaitProbeModule(sub, zero, 0))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				loaded := publicArtifactRoundTrip(t, c)
				defer loaded.Close()
				memory, err := NewSharedMemory(1, 1)
				if err != nil {
					t.Fatal(err)
				}
				defer memory.Close()
				in, err := Instantiate(loaded, NewImports().Memory("env", "memory", memory))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for _, expected := range []uint64{0, 1} {
					for _, timeout := range []uint64{0, 1} {
						want := uint64(0)
						if sub != 0 {
							want = 2 - expected
						}
						got, err := in.Invoke("run", 0, expected, timeout)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("value=%d timeout=%d: %v,%v; want %d", expected, timeout, got, err, want)
						}
					}
				}
			})
		}
	}
}
