//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64StateOnlyExternrefGlobalSource(export bool) []byte {
	sections := [][]byte{wasmtest.Section(6, wasmtest.Vec(
		wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
	))}
	if export {
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0))))
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicStateOnlyExternrefGlobalSource(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-externref-global", rv64StateOnlyExternrefGlobalSource(true), artifact)
			if !rv64StateOnlyExternrefGlobal(c) || c.requiredFeatures != CoreFeatureReferenceTypes || compiledStructuralRequiredFeatures(c) != CoreFeatureReferenceTypes {
				t.Fatal("externref state-only metadata contract")
			}
			rv64CheckStateOnlyCode(t, c, 0)
			if len(c.Globals) != 1 || c.Globals[0].Type != ValExternRef || c.Globals[0].Mutable || c.Globals[0].Bits != 0 || len(c.ExportedGlobals()) != 1 || c.ExportedGlobals()[0] != "g" {
				t.Fatalf("externref global metadata = %#v exports=%v", c.Globals, c.ExportedGlobals())
			}

			instances := make([]*Instance, 0, 2)
			objects := make([]*Global, 0, 2)
			for range 2 {
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = in.Close() })
				instances = append(instances, in)
				got, err := in.GlobalValue("g")
				if err != nil || got.Type() != ValExternRef || !got.ExternRef().IsNull() {
					t.Fatalf("GlobalValue(g) = %v, %v; want null externref", got, err)
				}
				if err := in.SetGlobalValue("g", ValueExternRef(ExternRef{})); err == nil || !strings.Contains(err.Error(), "immutable") {
					t.Fatalf("immutable externref write error = %v", err)
				}
				object, err := in.ExportedGlobalObject("g")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = object.Close() })
				objects = append(objects, object)
				if value, err := object.GetValue(); err != nil || value.Type() != ValExternRef || !value.ExternRef().IsNull() {
					t.Fatalf("global object value = %v, %v", value, err)
				}
			}
			if objects[0] == objects[1] {
				t.Fatal("externref global storage shared between instances")
			}
			rv64CheckStateOnlyCode(t, c, 2)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			for i := range instances {
				if value, err := objects[i].GetValue(); err != nil || !value.ExternRef().IsNull() {
					t.Fatalf("global did not outlive compiled handle: %v, %v", value, err)
				}
				if err := objects[i].Close(); err != nil {
					t.Fatal(err)
				}
				if err := objects[i].Close(); err != nil {
					t.Fatal("repeated global close", err)
				}
				if err := instances[i].Close(); err != nil {
					t.Fatal(err)
				}
			}
			rv64CheckStateOnlyCode(t, c, 0)
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler unavailable in precompiled runtime")
	}
	global := func(typ wasm.ValType, mutable bool, init []byte) []byte {
		return wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(typ, mutable, init)))
	}
	for name, source := range map[string][]byte{
		"mutable":         wasmtest.Module(global(wasm.ExternRef, true, []byte{0xd0, 0x6f, 0x0b})),
		"wrong-heap":      wasmtest.Module(global(wasm.ExternRef, false, []byte{0xd0, 0x70, 0x0b})),
		"indexed-type":    wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 0, 0xd0, 0, 0x0b}))),
		"imported":        wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "g", wasm.ExternRef, false)))),
		"extra-global":    wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}), wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b})))),
		"table":           wasmtest.Module(global(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}), wasmtest.Section(4, []byte{1, 0x70, 0, 0})),
		"function":        wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), global(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b})))),
		"initializer-ref": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0x23, 0, 0x0b})))),
	} {
		t.Run(name, func(t *testing.T) {
			if c, err := Compile(nil, source); c != nil || err == nil {
				if c != nil {
					_ = c.Close()
				}
				t.Fatal("unsupported externref global source admitted", err)
			}
		})
	}
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	if c, err := Compile(cfg, rv64StateOnlyExternrefGlobalSource(true)); c != nil || err == nil || !strings.Contains(err.Error(), "reference-types disabled") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("disabled reference-types admitted", err)
	}
}

func TestRV64PublicStateOnlyExternrefGlobalArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-externref-global-admission", rv64StateOnlyExternrefGlobalSource(true), true)
	defer c.Close()
	for name, mutate := range map[string]func(*Compiled){
		"wrong-type":         func(v *Compiled) { v.Globals[0].Type = ValFuncRef },
		"missing-value-type": func(v *Compiled) { v.Globals[0].HasValueType = false },
		"value-type-index":   func(v *Compiled) { v.Globals[0].ValueTypeIndex = 1 },
		"extra-value-type":   func(v *Compiled) { v.ValueTypes = append(v.ValueTypes, ValueTypeDescriptor{Kind: ValueTypeI32}) },
		"mutable":            func(v *Compiled) { v.Globals[0].Mutable = true },
		"nonnull-bits":       func(v *Compiled) { v.Globals[0].Bits = 1 },
		"vector-payload":     func(v *Compiled) { v.Globals[0].V128[0] = 1 },
		"init-global":        func(v *Compiled) { v.Globals[0].HasInitGlobal = true },
		"inactive-global":    func(v *Compiled) { v.Globals[0].InitGlobal = 1 },
		"init-function":      func(v *Compiled) { v.Globals[0].HasInitFunc = true },
		"inactive-function":  func(v *Compiled) { v.Globals[0].InitFunc = 1 },
		"init-expression":    func(v *Compiled) { v.Globals[0].InitExpr = []byte{0xd0, 0x6f, 0x0b} },
		"missing-feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"extra-feature":      func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
		"function-entry":     func(v *Compiled) { v.Funcs = []FuncSig{{}} },
		"code-entry":         func(v *Compiled) { v.Entry = []int{0} },
		"global-import":      func(v *Compiled) { v.GlobalImports = []GlobalImportDef{{Type: ValExternRef}} },
		"extra-global":       func(v *Compiled) { v.Globals = append(v.Globals, v.Globals[0]) },
		"wrong-export":       func(v *Compiled) { v.GlobalExports["g"] = 1 },
		"nonnullable-type":   func(v *Compiled) { v.ValueTypes[0].Ref.Nullable = false },
		"table":              func(v *Compiled) { v.HasTable, v.TableSize = true, 1 },
		"passive-data":       func(v *Compiled) { v.PassiveData = []PassiveDataInit{{Bytes: []byte{1}}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Globals = append([]GlobalDef(nil), bad.Globals...)
			bad.GlobalImports = append([]GlobalImportDef(nil), bad.GlobalImports...)
			bad.ValueTypes = append([]ValueTypeDescriptor(nil), bad.ValueTypes...)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("hostile externref state-only artifact admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "missing-feature" || name == "vector-payload" || name == "inactive-global" || name == "inactive-function" {
				if err != nil || loaded == nil || loaded.requiredFeatures != CoreFeatureReferenceTypes {
					t.Fatal("canonical externref artifact metadata was not restored", loaded, err)
				}
				if !rv64StateOnlyExternrefGlobal(loaded) {
					t.Fatal("canonical externref artifact did not clear inactive metadata")
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile externref state-only artifact round-trip admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalCompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		t.Skip("actual precompiled runtime only")
	}
	if c, err := Compile(nil, rv64StateOnlyExternrefGlobalSource(true)); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("compiler-free externref witness used source compilation", err)
	}
}
