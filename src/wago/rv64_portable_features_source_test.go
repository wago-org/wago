//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage && !wago_precompiled

package wago

import (
	"testing"

	rv "github.com/wago-org/wago/codegen/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PreparePortableFeature(t *testing.T, name string) []byte {
	t.Helper()
	var c *Compiled
	var err error
	switch name {
	case "gc-memory-shared", "gc-memory-shared-call":
		c, err = Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit), gcMemoryCombinationCallSource(false, true, 2, name == "gc-memory-shared-call"))
	case "gc-memory32", "gc-memory64", "gc-memory32-unbounded", "gc-memory64-unbounded", "gc-memory32-large":
		wide := name == "gc-memory64" || name == "gc-memory64-unbounded"
		maximum := uint32(2)
		if name == "gc-memory32-unbounded" || name == "gc-memory64-unbounded" {
			maximum = 0
		}
		if name == "gc-memory32-large" {
			maximum = 257
		}
		c, err = Compile(NewRuntimeConfig(), gcMemoryCombinationSource(wide, false, maximum))
	case "reference-call", "reference-tail":
		cfg := rv64NativeTailConfig()
		c, err = Compile(cfg, rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, false, name == "reference-call"))
	case "indexed-table-tail":
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 0}, []byte{0x6f, 0, 0}, []byte{0x70, 0, 0}, []byte{0x70, 0, 1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
			wasmtest.Section(9, wasmtest.Vec([]byte{2, 3, 0x41, 0, 0x0b, 0, 1, 0})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x13, 0, 3, 0x0b}))),
		)
		c, err = Compile(rv64NativeTailConfig(), source)
	case "unreachable-reference-tail":
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("trap", 0, 0), wasmtest.ExportEntry("good", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x15, 0, 0x0b}), wasmtest.Code([]byte{0x41, 42, 0x0b}))),
		)
		c, err = Compile(rv64NativeTailConfig(), source)
	case "rooted-tail":
		cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall)
		cfg.railshotState.tailProducts = true
		c, err = Compile(cfg, rv64PublicRootedLocalTailModule(false))
	case "segment-arrays":
		c, err = Compile(rv64I31Config(), rootedScalarSegmentArrayModule())
	case "exceptions":
		c, err = compileStagedExceptionHandlingFeaturesForTest(stagedExceptionHandlingModule(), false)
	case "owned-exceptions":
		cfg := rv64I31Config()
		features := cfg.frontendFeatures()
		features.ExceptionHandling, features.ExceptionReferences = true, true
		c, err = compileWithFrontendFeatures(cfg, ownedExceptionPublicModule(), features)
	case "function-struct", "function-array":
		c, err = Compile(rv64I31Config(), rv64FunctionStorageFixture(name == "function-array", true, false, "set"))
	case "owned-functions":
		cfg := rv64I31Config()
		features := cfg.frontendFeatures()
		features.ExceptionHandling, features.ExceptionReferences = true, true
		c, err = compileWithFrontendFeatures(cfg, rv64OwnedFunctionExceptionFixture("local"), features)
	case "owned-null":
		cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
		features := cfg.frontendFeatures()
		features.ExceptionHandling, features.ExceptionReferences = true, true
		c, err = compileWithFrontendFeatures(cfg, rv64ExceptionMixedNullModule(true, true), features)
	case "threads":
		c, err = Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit), sharedAtomicWaitNotifyModule())
	case "table64-mixed":
		c, err = Compile(rv64NativeTailConfig(), rv64GenericTable64MixedSource())
	case "table64-capacity":
		c, err = Compile(rv64NativeTailConfig(), rv64GenericTable64CapacitySource(0, ^uint64(0)))
	case "plugin-v128":
		rt := NewRuntime()
		defer rt.Close()
		if err := rt.Use(rv64CustomCodegenExtension{}); err != nil {
			t.Fatal(err)
		}
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}), wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("rv64-custom", "new", 0), instructionFuncImport("rv64-custom", "low", 1))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x10, 1, 0x0b}))))
		module, err := rt.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		defer module.Close()
		blob, err := module.c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return blob
	case "plugin":
		calls := 0
		ext := instructionMachineExt{name: "i32.identity", input: []int32{32}, output: []int32{32}, portableCalls: &calls, lowering: &rv.Lowering{Compatibility: rv.CompatibilityManaged, Managed: func(ctx rv.ManagedContext) error {
			reg, err := ctx.InputI32(0)
			if err != nil {
				return err
			}
			return ctx.OutputI32(reg)
		}}}
		rt, module := compileMachineInstruction(t, ext)
		defer rt.Close()
		defer module.Close()
		blob, err := module.c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return blob
	default:
		t.Fatal("unknown portable feature", name)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return blob
}
