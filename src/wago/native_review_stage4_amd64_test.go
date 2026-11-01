//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func nativeReviewStage4Module() []byte {
	body := []byte{0, 0x41, 55, 0xfb, 0, 0, 0x24, 0}
	for i := 0; i < 40; i++ {
		body = append(body, 0x20, 0, 0xfb, 7, 1, 0x24, 1, 0x41, byte(i), 0xfb, 0, 0, 0x24, 0)
	}
	body = append(body, 0x23, 0, 0xfb, 2, 0, 0, 0x23, 1, 0xfb, 0x0f, 0x6a, 0x0b)
	read := []byte{0, 0x23, 0, 0xfb, 2, 0, 0, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("read", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), append(wasmtest.ULEB(uint32(len(read))), read...))))
}

// Differential policy/native controls with alternating widths and failed huge
// allocations. A pre-trap global publication must survive explicit collection.
func TestNativeReviewStage4MixedBoundsTrap(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, shift := range []uint8{0, 6, 10, 12} {
			t.Run(fmt.Sprintf("native=%v/shift=%d", native, shift), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-alloc", native).WithOptimization("dead-gc-new", false).WithOptimization("gc-lifetime-analyze", false), nativeReviewStage4Module())
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 8192, NurseryRegionShift: shift, LargeObjectBytes: 128, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				var consumed uint32
				for round := 0; round < 3; round++ {
					for _, n := range []uint64{0, 1, 9, 15, 16, 17, 27, 28, 63, 64, 65, 1024, 0xffffffff, 1} {
						before := in.gc.Stats().Allocations
						got, err := in.Invoke("run", n)
						if n == 0xffffffff {
							if err == nil {
								t.Fatal("huge unsigned length did not trap")
							}
							if shift != 0 && nativeRegionState(in, gc.NativeStructAllocCountOffset) != 0 {
								t.Fatal("trap retained reservation")
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
							v, err := in.Invoke("read")
							if err != nil || len(v) != 1 || v[0] != 55 {
								t.Fatalf("trap publication lost: %v %v", v, err)
							}
						} else {
							if err != nil || len(got) != 1 || got[0] != 39+n {
								t.Fatalf("n=%d: %v %v", n, got, err)
							}
							consumed += nativeRegionState(in, gc.NativeStructAllocCursorOffset)
							// Read Stats reconciles baseline's deferred struct accounting too.
							if delta := in.gc.Stats().Allocations - before; delta != 81 {
								t.Fatalf("n=%d allocations=%d", n, delta)
							}
						}
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
					}
				}
				if native && shift == 12 && consumed == 0 {
					t.Fatal("no generated publication observed")
				}
			})
		}
	}
}
