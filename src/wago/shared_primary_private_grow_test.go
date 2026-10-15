//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPrivateSecondaryGrowPreservesSharedPrimaryBase(t *testing.T) {
	if !platformCoreFeatures().IsEnabled(CoreFeatureThreads | CoreFeatureMultiMemory) {
		t.Skip("threads and multi-memory are unavailable on this platform")
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1}, []byte{1, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0x40, 1, 0x1a, // drop(memory.grow 1 (local.get 0))
			0x41, 0, 0x41, 42, 0xfe, 0x17, 2, 0, // i32.atomic.store 0, 42
			0x41, 0, 0xfe, 0x10, 2, 0, // i32.atomic.load 0
			0x3f, 1, 0x6a, 0x0b, // add secondary memory.size
		}))),
	)
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureMultiMemory|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("shared-memory-base", enabled).Compile(module)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for index, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
			if code != c {
				defer code.Close()
			}
			in, err := Instantiate(code, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for step, delta := range []uint64{0, 1, 1} {
				got, invokeErr := in.Invoke("run", delta)
				want := uint64(44)
				if step == 0 {
					want = 43
				}
				if invokeErr != nil || len(got) != 1 || got[0] != want {
					in.Close()
					t.Fatalf("shared-base=%t artifact=%d step=%d: got=%v want=%d err=%v", enabled, index, step, got, want, invokeErr)
				}
			}
			in.Close()
		}
	}
}
