//go:build amd64

package dragline

import (
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestAMD64PublishesDirectPreparedLeafAcrossCompilerPaths(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsSignals,
		ConfigurationFingerprint: [32]byte{1},
	}
	assertDirect := func(t *testing.T, output corecompiler.Output) {
		t.Helper()
		if len(output.DirectPrepared) == 0 || output.DirectPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted direct prepared metadata")
		}
		if len(output.DirectLeafPrepared) == 0 || output.DirectLeafPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted direct leaf metadata")
		}
	}

	for _, workers := range []int{1, 2} {
		t.Run(map[int]string{1: "sequential", 2: "parallel"}[workers], func(t *testing.T) {
			input.FunctionWorkers = workers
			output, err := (Compiler{}).Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			assertDirect(t, output)
		})
	}

	input.FunctionWorkers = 1
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	compiler := Compiler{FunctionCache: cache}
	for _, name := range []string{"cold-cache", "warm-cache"} {
		t.Run(name, func(t *testing.T) {
			output, err := compiler.Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			assertDirect(t, output)
		})
	}
}
