//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MutableFuncrefClearModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}))),
	)
}

func TestRV64PublicMutableFuncrefGlobalClear(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			producerCode := rv64TableStageCompile(t, "mutable-funcref-clear-producer", rv64NativeTokenModule(false, true, true, false), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			value, err := producer.GlobalValue("reference")
			if err != nil || value.FuncRef().IsNull() {
				t.Fatal("producer token", value, err)
			}
			shared, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			if err = shared.SetValue(value); err != nil {
				t.Fatal(err)
			}
			compiled := rv64TableStageCompile(t, "mutable-funcref-clear-consumer", rv64MutableFuncrefClearModule(), artifact)
			defer compiled.Close()
			view := compiled.executionView()
			if !(rv64MutableFuncrefGlobalClear(view) || rv64PrimitiveReferenceContract(view)) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("mutable funcref clear witness missing: trap=%t tablectx=%t tableref=%t regoff=%t table64=%d header=%t exports=%t desc=%t escape=%t refs=%t/%t/%t tails=%t imports=%d/%d/%d funcs=%+v ids=%v exports=%v types=%+v descs=%+v globals=%+v globalimports=%+v globalexports=%v values=%+v entries=%v/%v features=%#x structural=%#x sync=%d codeok=%t", view.nativeContextTrapABI, view.nativeTableContextABI, view.nativeTableReferenceABI, view.registerABIDisabled, view.nativeTable64ABI, view.needsFuncRefContextHeader, view.hasTableExportMetadata, view.NeedsFuncRefDescs, view.dynamicFuncrefEscape, view.nativeReferenceFunctionABI, view.nativeReferenceCallABI, view.nativeReferenceControlABI, rv64HasReferenceTail(view), view.NumImports, len(view.Imports), len(view.importFuncSigs), view.Funcs, view.FuncTypeID, view.Exports, view.Types, view.GCTypeDescs, view.Globals, view.GlobalImports, view.GlobalExports, view.ValueTypes, view.Entry, view.InternalEntry, view.requiredFeatures, compiledStructuralRequiredFeatures(view), view.syncHostSlots, rv64MutableFuncrefGlobalClearExecutionCode(view))
			}
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module, WithImports(NewImports().Global("env", "cell", shared)))
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if err = producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			rootsBeforeClear := producer.referenceLifetime().snapshot().ResourceRoots
			if _, err = consumer.Invoke("clear"); err != nil {
				t.Fatal("guest clear", err)
			}
			got, err := shared.GetValue()
			if err != nil || !got.FuncRef().IsNull() {
				t.Fatal("guest clear did not publish null", got, err)
			}
			rootsAfterClear := producer.referenceLifetime().snapshot().ResourceRoots
			if rootsBeforeClear == 0 || rootsAfterClear+1 != rootsBeforeClear {
				t.Fatal("guest clear did not retire exactly one descriptor-owner root", rootsBeforeClear, rootsAfterClear)
			}
			shared.owner.mu.Lock()
			retained := len(shared.owner.retained)
			shared.owner.mu.Unlock()
			if retained != 0 {
				t.Fatal("guest clear retained a stale descriptor owner", retained)
			}
			if _, err = consumer.Invoke("clear"); err != nil {
				t.Fatal("idempotent guest clear", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalClearAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-clear-admission", rv64MutableFuncrefClearModule(), true)
	defer compiled.Close()
	if !(rv64MutableFuncrefGlobalClear(compiled.executionView()) || rv64PrimitiveReferenceContract(compiled.executionView())) {
		t.Fatal("base mutable funcref clear witness rejected")
	}
	if err := rv64ReferenceImportTailCopy(compiled).validate(); err != nil {
		t.Fatal("valid copied metadata rejected", err)
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":            func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export": func(v *Compiled) { v.Exports = map[string]int{"other": 0} },
		"global-export":   func(v *Compiled) { v.GlobalExports = map[string]int{"other": 0} },
		"immutable":       func(v *Compiled) { v.GlobalImports[0].Mutable = false; v.Globals[0].Mutable = false },
		"signature": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[0].Results = []ValType{ValI32}
		},
		"function-type-id": func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[0] ^= 1 },
		"type-descriptor":  func(v *Compiled) { v.Types = slices.Clone(v.Types); v.Types[0].Final = false },
		"gc-descriptor":    func(v *Compiled) { v.GCTypeDescs = slices.Clone(v.GCTypeDescs); v.GCTypeDescs[0].Final = false },
		"entry":            func(v *Compiled) { v.Entry = []int{4} },
		"feature":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMutableGlobal },
		"abi":              func(v *Compiled) { v.nativeTableReferenceABI = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := rv64ReferenceImportTailCopy(compiled)
			bad.GlobalImports = slices.Clone(bad.GlobalImports)
			bad.Globals = slices.Clone(bad.Globals)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(bad)
			if err := bad.validate(); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutable clear artifact admitted", err)
			}
		})
	}
}
