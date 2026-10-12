//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MutableFuncrefLocalCallModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			wasmtest.Code([]byte{0x41, 57, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalCallScalarModule(typ wasm.ValType, targetBody []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code(targetBody),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalCallI32Module() []byte {
	return rv64MutableFuncrefLocalCallScalarModule(wasm.I32, []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b})
}

func TestRV64PublicMutableFuncrefGlobalLocalCall(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			cfg := rv64NativeTailConfig()
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-producer", rv64PublicReferenceFunctionCallBoundaryModule(51), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			foreignBits, err := producer.Invoke("get")
			if err != nil || len(foreignBits) != 1 || foreignBits[0] == 0 {
				t.Fatal("foreign producer token", foreignBits, err)
			}
			foreign := ValueOf(ValFuncRef, foreignBits[0])

			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call", rv64MutableFuncrefLocalCallModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalCall(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("local-call witness missing: code=%d/%x entries=%v/%v refs=%t/%t/%t escape=%t globals=%+v values=%+v exports=%v/%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.nativeReferenceFunctionABI, view.nativeReferenceCallABI, view.nativeReferenceControlABI, view.dynamicFuncrefEscape, view.Globals, view.ValueTypes, view.Exports, view.GlobalExports, railshotValidateCompiled(view))
			}
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module)
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			assertCall := func(want uint64) {
				t.Helper()
				got, invokeErr := consumer.Invoke("call")
				if invokeErr != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("mutable funcref call_ref", got, invokeErr, "want", want)
				}
			}
			if got, invokeErr := consumer.Invoke("call"); got != nil {
				t.Fatal("null call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("null mutable funcref call trap", invokeErr)
				}
			}
			if _, err = consumer.Invoke("set_0"); err != nil {
				t.Fatal("install local zero", err)
			}
			assertCall(42)
			if _, err = consumer.Invoke("set_1"); err != nil {
				t.Fatal("install local one", err)
			}
			assertCall(57)

			before := producer.referenceLifetime().snapshot()
			if err = consumer.SetGlobalValue("cell", foreign); err != nil {
				t.Fatal("install foreign typed target", err)
			}
			installed := producer.referenceLifetime().snapshot()
			if installed.ResourceRoots != before.ResourceRoots+1 {
				t.Fatal("host ingress did not retain foreign call target", before, installed)
			}
			assertCall(51)
			if err = producer.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources {
				t.Fatal("typed cell did not retain closed foreign target", parked)
			}
			assertCall(51)
			if _, err = consumer.Invoke("set_0"); err != nil {
				t.Fatal("replace foreign target", err)
			}
			assertCall(42)
			if retired := producer.referenceLifetime().snapshot(); retired.ResourceRoots+1 != parked.ResourceRoots {
				t.Fatal("foreign target replacement did not retire exactly one cell root", parked, retired)
			}
			if _, err = consumer.Invoke("clear"); err != nil {
				t.Fatal("clear typed cell", err)
			}
			if got, invokeErr := consumer.Invoke("call"); got != nil {
				t.Fatal("cleared call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("cleared mutable funcref call trap", invokeErr)
				}
			}
			if err = producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = compiled.Close(); err != nil {
				t.Fatal(err)
			}
			if err = rt.Close(); err != nil {
				t.Fatal(err)
			}
			if final := producer.referenceLifetime().snapshot(); final.PhysicalResources || final.ResourceRoots != 0 {
				t.Fatal("runtime token release did not retire foreign call target", final)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallI32(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()

			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-i32", rv64MutableFuncrefLocalCallI32Module(), artifact)
			if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
				t.Fatal("i32 local-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			assertTrap := func() {
				t.Helper()
				got, invokeErr := consumer.Invoke("call", 0xdeadbeef80000001)
				if got != nil {
					t.Fatal("null i32 call_ref returned a result", got, invokeErr)
				}
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("null i32 mutable funcref call trap", invokeErr)
				}
			}
			assertCall := func(input, want uint64) {
				t.Helper()
				got, invokeErr := consumer.Invoke("call", input)
				if invokeErr != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("mutable i32 funcref call_ref", got, invokeErr, "want", want)
				}
			}
			assertTrap()
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install i32 identity", err)
			}
			assertCall(0xdeadbeef80000001, 0x80000001)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install i32 increment", err)
			}
			assertCall(0xcafebabeffffffff, 0)

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-i32-producer", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign i32 producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign i32 producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign i32 target", err)
			}
			assertCall(0xfeedface80000002, 0x80000002)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear i32 target", err)
			}
			assertTrap()

			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallScalars(t *testing.T) {
	tests := map[string]struct {
		typ       wasm.ValType
		body      []byte
		input     uint64
		identity  uint64
		increment uint64
	}{
		"i64": {wasm.I64, []byte{0x20, 0, 0x42, 1, 0x7c, 0x0b}, ^uint64(0), ^uint64(0), 0},
		"f32": {wasm.F32, []byte{0x20, 0, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x0b}, 0xdeadbeef00000000 | uint64(math.Float32bits(1.5)), uint64(math.Float32bits(1.5)), uint64(math.Float32bits(2.5))},
		"f64": {wasm.F64, []byte{0x20, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}, math.Float64bits(1.5), math.Float64bits(1.5), math.Float64bits(2.5)},
	}
	for name, test := range tests {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", name, artifact), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()
				compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-"+name, rv64MutableFuncrefLocalCallScalarModule(test.typ, test.body), artifact)
				if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
					t.Fatal("scalar local-call witness missing")
				}
				consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
				assertCall := func(want uint64) {
					t.Helper()
					got, err := consumer.Invoke("call", test.input)
					if err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal("mutable scalar funcref call_ref", got, err, "want", want)
					}
				}
				if _, err := consumer.Invoke("set_0"); err != nil {
					t.Fatal("install scalar identity", err)
				}
				assertCall(test.identity)
				if _, err := consumer.Invoke("set_1"); err != nil {
					t.Fatal("install scalar arithmetic target", err)
				}
				assertCall(test.increment)

				producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-"+name+"-producer", rv64PublicNativeIdentityModule([]wasm.ValType{test.typ}), artifact)
				producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("foreign scalar producer descriptor")
				}
				token, err := rt.refStore.issue(producer, descriptor)
				if err != nil {
					t.Fatal("foreign scalar producer token", err)
				}
				if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
					t.Fatal("install foreign scalar target", err)
				}
				assertCall(test.identity)
				if _, err := consumer.Invoke("clear"); err != nil {
					t.Fatal("clear scalar target", err)
				}
				if got, invokeErr := consumer.Invoke("call", test.input); got != nil {
					t.Fatal("cleared scalar call_ref returned a result", got, invokeErr)
				} else {
					var trap *coreruntime.TrapError
					if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
						t.Fatal("cleared scalar mutable funcref call trap", invokeErr)
					}
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := producerCode.Close(); err != nil {
					t.Fatal(err)
				}
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallParkedClearRetainsTarget(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-parked-producer", rv64PublicReferenceFunctionCallBoundaryModule(51), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign producer descriptor")
			}
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-parked", rv64MutableFuncrefLocalCallModule(), artifact)
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			if err := consumer.ensureGuestLocalFuncrefGlobalOwner(0); err != nil {
				t.Fatal("install local cell owner", err)
			}
			if err := consumer.globalCells[0].copyGuestFuncrefDescriptor(consumer, descriptor); err != nil {
				t.Fatal("install raw foreign typed target", err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != 1 {
				t.Fatal("cell did not become the sole closed-producer root", parked)
			}

			// Model the portal sequence exactly: acquire targets while native
			// execution is leased, park by dropping that lease, and recursively
			// enter the consumer from the host side to clear the mutable cell.
			nativeExecutionMu.Lock()
			lease, err := railshotAcquireNativeTableTargets(consumer)
			nativeExecutionMu.Unlock()
			if err != nil {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("acquire outer mutable-call activation", err)
			}
			if active := producer.referenceLifetime().snapshot(); active.ResourceRoots != parked.ResourceRoots+1 {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("outer activation did not retain its call target", parked, active)
			}
			if _, err := consumer.Invoke("clear"); err != nil {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("callback reentry clear", err)
			}
			if suspended := producer.referenceLifetime().snapshot(); !suspended.PhysicalResources || suspended.ResourceRoots != 1 {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("parked activation lost its replaced call target", suspended)
			}
			nativeExecutionMu.Lock()
			railshotRefreshNativeTableTargets(lease)
			nativeExecutionMu.Unlock()
			if resumed := producer.referenceLifetime().snapshot(); !resumed.PhysicalResources || resumed.ResourceRoots != 1 {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("resumed activation lost its original call target", resumed)
			}
			railshotReleaseNativeTableTargets(lease)
			if retired := producer.referenceLifetime().snapshot(); !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("outer activation did not retire its replaced call target", retired)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallAdmission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-admission", rv64MutableFuncrefLocalCallModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
		t.Fatal("base local-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export": func(v *Compiled) {
			v.Exports = map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "clear": 4, "other": 5}
		},
		"global-export":    func(v *Compiled) { v.GlobalExports = map[string]int{"other": 0} },
		"immutable":        func(v *Compiled) { v.Globals[0].Mutable = false },
		"untyped-global":   func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Defined = false },
		"missing-call-abi": func(v *Compiled) { v.nativeReferenceCallABI = false },
		"missing-control":  func(v *Compiled) { v.nativeReferenceControlABI = false },
		"function-type-id": func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[5] ^= 1 },
		"entry":            func(v *Compiled) { v.Entry = []int{0, 608, 1216, 1936, 2656, 3316} },
		"feature":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Globals = slices.Clone(bad.Globals)
			bad.ValueTypes = slices.Clone(bad.ValueTypes)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile local-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallI32Admission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-i32-admission", rv64MutableFuncrefLocalCallI32Module(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
		t.Fatal("base i32 local-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"parameter": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
			v.Funcs[5].Params[0] = ValI64
		},
		"type-parameter": func(v *Compiled) {
			v.Types = slices.Clone(v.Types)
			v.Types[0].Params = slices.Clone(v.Types[0].Params)
			v.Types[0].Params[0] = ValueTypeDescriptor{Kind: ValueTypeI64}
		},
		"entry": func(v *Compiled) { v.Entry = []int{0, 608, 1264, 1984, 2704, 3364} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Types = slices.Clone(bad.Types)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile i32 local-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallScalarAdmission(t *testing.T) {
	modules := map[string][]byte{
		"i64": rv64MutableFuncrefLocalCallScalarModule(wasm.I64, []byte{0x20, 0, 0x42, 1, 0x7c, 0x0b}),
		"f32": rv64MutableFuncrefLocalCallScalarModule(wasm.F32, []byte{0x20, 0, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x0b}),
		"f64": rv64MutableFuncrefLocalCallScalarModule(wasm.F64, []byte{0x20, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
	}
	for name, source := range modules {
		t.Run(name, func(t *testing.T) {
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-"+name+"-admission", source, false)
			defer compiled.Close()
			if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
				t.Fatal("base scalar local-call witness rejected")
			}
			for mutation, mutate := range map[string]func(*Compiled){
				"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
				"parameter": func(v *Compiled) {
					v.Funcs = slices.Clone(v.Funcs)
					v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
					v.Funcs[5].Params[0] = ValI32
				},
				"type-result": func(v *Compiled) {
					v.Types = slices.Clone(v.Types)
					v.Types[0].Results = slices.Clone(v.Types[0].Results)
					v.Types[0].Results[0] = ValueTypeDescriptor{Kind: ValueTypeI32}
				},
				"entry": func(v *Compiled) { v.Entry = slices.Clone(v.Entry); v.Entry[5] += 4 },
			} {
				t.Run(mutation, func(t *testing.T) {
					bad := *compiled.executionView()
					bad.Funcs = slices.Clone(bad.Funcs)
					bad.Types = slices.Clone(bad.Types)
					mutate(&bad)
					if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("hostile scalar local-call artifact admitted", err)
					}
				})
			}
		})
	}
}
