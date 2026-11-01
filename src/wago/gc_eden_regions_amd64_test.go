//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// Run actual allocating guest code with a live caller local across a callee's
// region exhaustion. The compiler's lifetime consumer must safely fall back
// when the collector refuses contiguous native allocation reservations.
func TestGCEdenRegionsCallerRoot(t *testing.T) {
	callee := []byte{1, 2, 0x63, 0, 0x41, 42, 0xfb, 0, 0, 0x21, 0, 0x20, 0, 0x22, 1, 0x1a, 0x01, 0x20, 1, 0xfb, 2, 0, 0, 0x0b}
	caller := []byte{2, 1, 0x63, 0, 1, 0x7f,
		0x41, 7, 0xfb, 0, 0, 0x21, 0, 0x41, 0, 0x21, 1, 0x03, 0x40,
		0x10, 0, 0x1a, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x41, 32, 0x49, 0x0d, 0,
		0x0b, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(callee))), callee...), append(wasmtest.ULEB(uint32(len(caller))), caller...))))
	for _, on := range []bool{false, true} {
		for _, shift := range []uint8{0, 6} {
			t.Run(fmt.Sprintf("lifetime=%v/regionShift=%d", on, shift), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("gc-lifetime-analyze", on), module)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 128, SurvivorBytes: 128, NurseryRegionShift: shift, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for n := 0; n < 32; n++ {
					got, err := in.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 7 {
						t.Fatalf("run=%v err=%v", got, err)
					}
				}
				stats := in.gc.Stats()
				if shift != 0 && (stats.FullCollections == 0 || stats.Allocations < 1024) {
					t.Fatalf("region runtime not exercised: %+v", stats)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
