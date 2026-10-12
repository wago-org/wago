//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

func rv64MutableFuncrefGetModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
}

func TestRV64PublicMutableFuncrefGlobalGet(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			producerCode := rv64TableStageCompile(t, "mutable-funcref-get-producer", rv64NativeTokenModule(false, true, true, false), artifact)
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
			compiled := rv64TableStageCompile(t, "mutable-funcref-get-consumer", rv64MutableFuncrefGetModule(), artifact)
			defer compiled.Close()
			if !rv64MutableFuncrefGlobalGet(compiled.executionView()) {
				t.Fatal("mutable funcref get witness missing")
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
			exported, err := consumer.ExportedGlobalObject("cell")
			if err != nil || exported != shared {
				t.Fatal("re-export identity", exported == shared, err)
			}
			assertNull := func(want uint64) {
				t.Helper()
				got, err := consumer.Invoke("is_null")
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatal("guest mutable global.get/ref.is_null", got, err, "want", want)
				}
			}
			assertNull(1)
			if err := shared.SetValue(value); err != nil {
				t.Fatal(err)
			}
			assertNull(0)
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			assertNull(0)
			if err := shared.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal(err)
			}
			assertNull(1)
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalGetAdmission(t *testing.T) {
	compiled := rv64TableStageCompile(t, "mutable-funcref-get-admission", rv64MutableFuncrefGetModule(), true)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalGet(compiled.executionView()) {
		t.Fatal("base mutable funcref get witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":            func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export": func(v *Compiled) { v.Exports = map[string]int{"other": 0} },
		"global-export":   func(v *Compiled) { v.GlobalExports = map[string]int{"other": 0} },
		"immutable":       func(v *Compiled) { v.GlobalImports[0].Mutable = false; v.Globals[0].Mutable = false },
		"signature": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[0].Results = slices.Clone(v.Funcs[0].Results)
			v.Funcs[0].Results[0] = ValI64
		},
		"function-type-id": func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[0] ^= 1 },
		"type-descriptor":  func(v *Compiled) { v.Types = slices.Clone(v.Types); v.Types[0].Final = false },
		"gc-descriptor":    func(v *Compiled) { v.GCTypeDescs = slices.Clone(v.GCTypeDescs); v.GCTypeDescs[0].Final = false },
		"entry":            func(v *Compiled) { v.Entry = []int{4} },
		"feature":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMutableGlobal },
		"abi":              func(v *Compiled) { v.nativeTableReferenceABI = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.GlobalImports = slices.Clone(bad.GlobalImports)
			bad.Globals = slices.Clone(bad.Globals)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutable get artifact admitted", err)
			}
		})
	}
}
