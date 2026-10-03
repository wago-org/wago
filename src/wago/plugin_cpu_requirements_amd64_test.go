//go:build amd64 && !tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	amd64codegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

const testPluginFeaturePOPCNT = amd64codegen.Features(1 << 9)

func TestFullAccessPluginPOPCNTRequirementArtifact(t *testing.T) {
	mockAMD64ArtifactCPU(t, shared.AMD64KnownFeatures)
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	ext := instructionMachineExt{name: "popcnt.marker", output: []int32{32}, lowering: &amd64codegen.Lowering{
		Compatibility: amd64codegen.CompatibilityFullAccess,
		Features:      testPluginFeaturePOPCNT,
		Emit: func(ctx amd64codegen.Context) error {
			r := ctx.AllocGP()
			ctx.Encoder().MovImm32(r, 1)
			ctx.Encoder().Popcnt(r, r, false)
			return ctx.OutputI32(r)
		},
	}}
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/machine", "popcnt.marker", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
	mod, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	compiled := mod.Compiled()
	if compiled.requiredAMD64Features != shared.AMD64POPCNT {
		t.Fatalf("requirements = %#x, want POPCNT", compiled.requiredAMD64Features)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	amd64CPUCache.features = shared.AMD64KnownFeatures &^ shared.AMD64POPCNT
	var missing Compiled
	if err := missing.UnmarshalBinary(blob); err == nil {
		missing.Close()
		t.Fatal("artifact requiring POPCNT admitted without POPCNT")
	}
	amd64CPUCache.features = shared.AMD64KnownFeatures
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.requiredAMD64Features != shared.AMD64POPCNT {
		t.Fatalf("roundtrip requirements = %#x, want POPCNT", loaded.requiredAMD64Features)
	}
}

func TestUnusedPluginCPURequirementsArtifact(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	for _, workers := range []int{1, 2} {
		for _, feature := range []amd64codegen.Features{amd64codegen.FeatureAVX2, amd64codegen.FeatureAVX512} {
			for _, tc := range []struct {
				name string
				body []byte
			}{
				{"unused", []byte{0x41, 1, 0x0b}},
				{"unreachable-call", []byte{0x00, 0x10, 0, 0x0b}},
			} {
				t.Run(fmt.Sprintf("%s/feature=%x/workers=%d", tc.name, feature, workers), func(t *testing.T) {
					// Simulate a detected SSE2 host. The fixture emits only scalar code.
					cachedAMD64CPUFeatures()
					originalFeatures, originalOK := amd64CPUCache.features, amd64CPUCache.ok
					t.Cleanup(func() { amd64CPUCache.features, amd64CPUCache.ok = originalFeatures, originalOK })
					amd64CPUCache.features, amd64CPUCache.ok = 0, true
					// Artifacts require explicit bounds checks, including guard-page builds.
					cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(workers)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					ext := instructionMachineExt{name: "avx.marker", output: []int32{32}, lowering: &amd64codegen.Lowering{
						Compatibility: amd64codegen.CompatibilityFullAccess,
						Features:      feature,
						Emit:          func(amd64codegen.Context) error { panic("unused plugin emitter called") },
					}}
					if err := rt.Use(ext); err != nil {
						t.Fatal(err)
					}
					module := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
						wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/machine", "avx.marker", 0))),
						wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x0b}), wasmtest.Code(tc.body))),
					)
					mod, err := rt.Compile(module)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					compiled := mod.Compiled()
					if compiled.requiredAMD64Features != 0 || compiled.RequiresAVX2() || compiled.RequiresAVX512() {
						t.Fatalf("unused plugin acquired CPU requirements %#x", compiled.requiredAMD64Features)
					}
					blob, err := compiled.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					var loaded Compiled
					if err := loaded.UnmarshalBinary(blob); err != nil {
						t.Fatalf("scalar artifact rejected on SSE2 host: %v", err)
					}
					defer loaded.Close()
					if loaded.requiredAMD64Features != 0 || loaded.RequiresAVX2() || loaded.RequiresAVX512() {
						t.Fatalf("artifact acquired CPU requirements %#x", loaded.requiredAMD64Features)
					}
					instance, err := rt.Instantiate(context.Background(), mod)
					if err != nil {
						t.Fatal(err)
					}
					defer instance.Close()
					out, err := instance.Invoke("run")
					if err != nil || len(out) != 1 || out[0] != 1 {
						t.Fatalf("scalar result = %v, %v; want 1", out, err)
					}
				})
			}
		}
	}
}
