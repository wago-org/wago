//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// No imports or callbacks are needed to collect while these Wasm locals are live.
func gcExternLocalRootsModule(op byte) []byte {
	typ := wasm.ExternRef
	if op == 27 {
		typ = wasm.AnyRef
	}
	body := []byte{1, 1, 0x63, 0, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 1, 0x20, 0, 0xfb, op, 0xd1, 0x1a}
	for i := 0; i < 3; i++ {
		body = append(body, 0xfb, 1, 0, 0x24, 0)
	}
	body = append(body, 0x20, 1, 0xfb, 2, 0, 0, 0x0b)
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(body)),
	)
}

func TestGCExternConversionKeepsLocalRoots(t *testing.T) {
	for _, op := range []byte{26, 27} {
		for _, profile := range []struct {
			name string
			gc   GCConfig
		}{
			{"throughput", GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
			{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
		} {
			t.Run(fmt.Sprintf("op%d/%s", op, profile.name), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternLocalRootsModule(op))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{GC: profile.gc})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for i := 0; i < 3; i++ {
					got, err := in.Invoke("run", 0)
					if err != nil || len(got) != 1 || got[0] != 73 {
						t.Fatalf("live local after collection: %v,%v; want73", got, err)
					}
				}
				admission := c.GCNativeRootAdmission()
				if !admission.Required || !admission.Exact || admission.Safepoints == 0 || admission.MaximumRoots == 0 {
					t.Fatalf("missing allocation roots: %+v", admission)
				}
			})
		}
	}
}
