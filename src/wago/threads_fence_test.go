//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestThreadsFenceRequiresNoMemory(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fence", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfe, 3, 0, 0x41, 42, 0x0b}))),
	)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV1 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.threadedMemory0() || c.memoryCount() != 0 {
		t.Fatal("fence synthesized shared memory")
	}
	loaded := publicArtifactRoundTrip(t, c)
	for _, compiled := range []*Compiled{c, loaded} {
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("fence")
		in.Close()
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("fence = %v, %v", got, err)
		}
	}
}
