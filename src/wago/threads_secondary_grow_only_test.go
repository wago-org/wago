//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func secondaryGrowOnlyModule(wide bool) []byte {
	typ, flags := wasm.I32, byte(3)
	if wide {
		typ = wasm.I64
		flags |= 4
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2}, []byte{flags, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 1, 0x0b}))),
	)
}

func TestSharedSecondaryGrowOnlyInitializesHelpers(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	for _, wide := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("wide=%t/regabi=%t", wide, regABI), func(t *testing.T) {
				c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI).Compile(secondaryGrowOnlyModule(wide))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				// Keep a missing requirement a test failure rather than a native null write.
				if !c.usesAtomicWaitHelpers() {
					t.Fatal("grow-only module omitted required helper context")
				}
				for _, compiled := range []*Compiled{c} {
					if !compiled.usesAtomicWaitHelpers() {
						t.Fatal("codec lost required grow helper")
					}
					in, err := Instantiate(compiled)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for _, step := range []struct{ delta, want uint64 }{{0, 1}, {1, 1}, {0, 2}, {1, ^uint64(0)}} {
						want := step.want
						if !wide {
							want = uint64(uint32(want))
						}
						got, err := in.Invoke("grow", step.delta, 11, 22, 33)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("delta=%d: %v,%v want=%x", step.delta, got, err, want)
						}
					}
				}
			})
		}
	}
}
