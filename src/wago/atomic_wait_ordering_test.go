//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep the epoch, completion flags, data, results, and futex on separate cache
// lines so that the test can expose store buffering between the workers.
const (
	atomicWaitOrderingEpoch = iota * 64
	atomicWaitOrderingDoneA
	atomicWaitOrderingDoneB
	atomicWaitOrderingDataA
	atomicWaitOrderingDataB
	atomicWaitOrderingResultA
	atomicWaitOrderingResultB
	atomicWaitOrderingFutex
)

// Each worker performs an ordinary store, a wait/notify at the same futex,
// and an atomic load of the other worker's data. Both loads cannot read zero:
// the queue operations must order the preceding stores and following loads.
// Atomic stores here would hide the missing ordering that this test detects.
func atomicWaitOrderingModule(literal bool, sub byte, iterations uint32) []byte {
	align, argType := byte(2), wasm.I32
	body := []byte{
		0x02, 0x40, 0x03, 0x40, // block; loop
		0x20, 0x05, 0x41, // iter; iterations
	}
	body = append(body, wasmtest.SLEB32(int32(iterations))...)
	body = append(body,
		0x4f, 0x0d, 0x01, // iter >= iterations: break
		0x20, 0x05, 0x41, 0x01, 0x6a, 0x21, 0x05, // ++iter
		0x03, 0x40, // spin until epoch == iter
		0x41, atomicWaitOrderingEpoch, 0xfe, 0x10, 0x02, 0x00,
		0x20, 0x05, 0x47, 0x0d, 0x00, 0x0b,
		0x20, 0x00, 0x41, 0x01, 0x36, 0x02, 0x00, // ordinary store own=1
		0x41,
	)
	body = append(body, wasmtest.SLEB32(atomicWaitOrderingFutex)...)
	if sub != 0 {
		argType = wasm.I64
		constant := byte(0x41)
		if sub == 2 {
			constant, align = 0x42, 3
		}
		body = append(body, constant, 0x00, 0x20, 0x04) // expected=0; dynamic timeout
	} else if literal {
		body = append(body, 0x41, 0x00) // literal count=0
	} else {
		body = append(body, 0x20, 0x04) // dynamic count
	}
	body = append(body, 0xfe, sub, align, 0x00, 0x1a) // wait/notify; drop
	body = append(body,
		0x20, 0x02, 0x20, 0x01, 0xfe, 0x10, 0x02, 0x00,
		0x36, 0x02, 0x00, // result = atomic_load(other)
		0x20, 0x03, 0x20, 0x05, 0xfe, 0x17, 0x02, 0x00, // atomic_store(done, iter)
		0x0c, 0x00, 0x0b, 0x0b, 0x0b,
	)
	entry := append([]byte{0x01, 0x01, 0x7f}, body...) // one i32 iteration local
	entry = append(wasmtest.ULEB(uint32(len(entry))), entry...)
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, argType}, nil))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
}

func TestAtomicWaitFastZeroOrdering(t *testing.T) {
	prior := runtime.GOMAXPROCS(3)
	defer runtime.GOMAXPROCS(prior)
	const iterations = 100000
	for _, tc := range []struct {
		name    string
		literal bool
		sub     byte
	}{
		{"notify-literal-zero", true, 0},
		{"notify-dynamic-zero", false, 0},
		{"wait32-dynamic-zero-timeout", false, 1},
		{"wait64-dynamic-zero-timeout", false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
			c, err := Compile(cfg, atomicWaitOrderingModule(tc.literal, tc.sub, iterations))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			m, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			a, err := Instantiate(c, testImports("env.memory", m))
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := Instantiate(c, testImports("env.memory", m))
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			// Bound stalled workers, not the speed of 100,000 guest/host rounds
			// on a loaded runner. Every round still checks the ordering outcome.
			const stallTimeout = 10 * time.Second
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 2)
			remaining := 2
			// Cancel and join both invocations before the instance/memory defers,
			// including when the host observes a timeout or an early worker exit.
			defer func() {
				cancel()
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				for remaining > 0 {
					select {
					case <-done:
						remaining--
					case <-timer.C:
						t.Error("workers did not stop after cancellation")
						return
					}
				}
			}()
			go func() {
				_, err := a.InvokeContext(ctx, "run", atomicWaitOrderingDataA, atomicWaitOrderingDataB, atomicWaitOrderingResultA, atomicWaitOrderingDoneA, 0)
				done <- err
			}()
			go func() {
				_, err := b.InvokeContext(ctx, "run", atomicWaitOrderingDataB, atomicWaitOrderingDataA, atomicWaitOrderingResultB, atomicWaitOrderingDoneB, 0)
				done <- err
			}()
			memory := m.UnsafeBytes()
			cell := func(off int) *uint32 { return (*uint32)(unsafe.Pointer(&memory[off])) }
			bothZero := 0
			for epoch := uint32(1); epoch <= iterations; epoch++ {
				deadline := time.Now().Add(stallTimeout)
				// Completion of both workers orders these resets after their prior
				// accesses. Publishing the epoch orders the resets before this round.
				atomic.StoreUint32(cell(atomicWaitOrderingDataA), 0)
				atomic.StoreUint32(cell(atomicWaitOrderingDataB), 0)
				atomic.StoreUint32(cell(atomicWaitOrderingEpoch), epoch)
				for spin := 0; atomic.LoadUint32(cell(atomicWaitOrderingDoneA)) != epoch || atomic.LoadUint32(cell(atomicWaitOrderingDoneB)) != epoch; spin++ {
					if spin&255 == 0 {
						runtime.Gosched()
						if time.Now().After(deadline) {
							t.Fatalf("epoch %d: workers did not complete within %s", epoch, stallTimeout)
						}
					}
				}
				if atomic.LoadUint32(cell(atomicWaitOrderingResultA)) == 0 && atomic.LoadUint32(cell(atomicWaitOrderingResultB)) == 0 {
					bothZero++
				}
			}
			completion := time.NewTimer(stallTimeout)
			defer completion.Stop()
			for remaining > 0 {
				select {
				case err := <-done:
					remaining--
					if err != nil {
						t.Fatal(err)
					}
				case <-completion.C:
					t.Fatal("workers did not return after the final epoch")
				}
			}
			t.Logf("both-zero outcomes = %d / %d", bothZero, iterations)
			if bothZero != 0 {
				t.Fatal("same-address wait/notify did not order ordinary stores before following atomic loads")
			}
		})
	}
}
