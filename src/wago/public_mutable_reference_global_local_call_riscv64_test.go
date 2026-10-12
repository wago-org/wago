//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
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
