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

// The stop flag bounds the probe if native context interruption fails.
func indexedWaitCancellationModule(address64 bool, index, sub byte) []byte {
	expected, constant := wasm.I32, byte(0x41)
	limits, other := byte(3), byte(5)
	if address64 {
		constant, limits, other = 0x42, 7, 1
	}
	align := byte(2)
	if sub == 2 {
		expected, align = wasm.I64, 3
	}
	memories := [][]byte{{limits, 1, 2}, {other, 0, 1}}
	if index != 0 {
		memories[0], memories[1] = memories[1], memories[0]
	}
	spin := []byte{constant, 16, 0x41, 1, 0xfe, 0x17, 0x42, index, 0, 0x03, 0x40,
		constant, 0, 0x20, 0, 0x20, 1, 0xfe, sub, align | 0x40, index, 0, 0x1a,
		constant, 16, 0xfe, 0x10, 0x42, index, 0, 0x41, 1, 0x46, 0x0d, 0, 0x0b, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{expected, wasm.I64}, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec(memories...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("memory", 2, uint32(index)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(spin))))
}

func TestIndexedAtomicWaitCancellation(t *testing.T) {
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, sub := range []byte{1, 2} {
				t.Run(fmt.Sprintf("addr64=%t/index=%d/op=%d", address64, index, sub), func(t *testing.T) {
					c, err := indexedWaitConfig().Compile(indexedWaitCancellationModule(address64, index, sub))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					memory, err := in.ExportedMemory("memory")
					if err != nil {
						t.Fatal(err)
					}
					flag := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[16]))
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					watchdog := time.AfterFunc(5*time.Second, cancel)
					defer watchdog.Stop()
					done := make(chan error, 1)
					go func() { _, err := in.InvokeContext(ctx, "spin", 1, ^uint64(0)); done <- err }()
					for atomic.LoadUint32(flag) == 0 {
						select {
						case err := <-done:
							t.Fatalf("spin returned before native entry:%v", err)
						case <-ctx.Done():
							atomic.StoreUint32(flag, 2)
							<-done
							t.Fatal("native entry timed out")
						default:
							runtime.Gosched()
						}
					}
					cancel()
					select {
					case err := <-done:
						if !errors.Is(err, context.Canceled) {
							t.Fatalf("cancel:%v", err)
						}
					case <-time.After(5 * time.Second):
						atomic.StoreUint32(flag, 2)
						select {
						case <-done:
						case <-time.After(5 * time.Second):
							t.Fatal("native loop ignored cancellation and the stop flag")
						}
						t.Fatal("native loop ignored cancellation")
					}
				})
			}
		}
	}
}
