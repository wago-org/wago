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

func rv64StateOnlyExternrefGlobalDirectorySource(count int, exports ...int) []byte {
	globals := make([][]byte, count)
	for i := range globals {
		globals[i] = wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b})
	}
	sections := [][]byte{wasmtest.Section(6, wasmtest.Vec(globals...))}
	if len(exports) != 0 {
		entries := make([][]byte, len(exports))
		for i, index := range exports {
			entries[i] = wasmtest.ExportEntry(fmt.Sprintf("g%d", index), byte(wasm.ExternGlobal), uint32(index))
		}
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(entries...)))
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicStateOnlyExternrefGlobalDirectory(t *testing.T) {
	all := make([]int, 256)
	for i := range all {
		all[i] = i
	}
	for _, tc := range []struct {
		name    string
		count   int
		exports []int
	}{
		{name: "two-none", count: 2},
		{name: "two-partial", count: 2, exports: []int{1}},
		{name: "two-complete", count: 2, exports: []int{0, 1}},
		{name: "maximum-complete", count: 256, exports: all},
	} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", tc.name, artifact), func(t *testing.T) {
				name := "state-only-externref-directory-" + tc.name
				c := rv64TableStageCompile(t, name, rv64StateOnlyExternrefGlobalDirectorySource(tc.count, tc.exports...), artifact)
				if !rv64StateOnlyExternrefGlobals(c) || len(c.Globals) != tc.count || len(c.ValueTypes) != 1 || len(c.GlobalExports) != len(tc.exports) {
					t.Fatal("externref global directory metadata contract")
				}
				rv64CheckStateOnlyCode(t, c, 0)
				instances := make([]*Instance, 0, 2)
				for range 2 {
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = in.Close() })
					instances = append(instances, in)
					if len(in.globalCells) != tc.count {
						t.Fatal("externref global cell count", len(in.globalCells), tc.count)
					}
					for i, cell := range in.globalCells {
						if cell == nil || len(cell.cell) != 8 {
							t.Fatal("externref global cell shape", i)
						}
						if i > 0 && cell == in.globalCells[i-1] {
							t.Fatal("externref globals alias storage", i)
						}
					}
					for _, index := range tc.exports {
						export := fmt.Sprintf("g%d", index)
						got, err := in.GlobalValue(export)
						if err != nil || got.Type() != ValExternRef || !got.ExternRef().IsNull() {
							t.Fatal("externref directory value", export, got, err)
						}
						if err := in.SetGlobalValue(export, ValueExternRef(ExternRef{})); err == nil || !strings.Contains(err.Error(), "immutable") {
							t.Fatal("externref directory mutation", export, err)
						}
					}
				}
				rv64CheckStateOnlyCode(t, c, 2)
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				for _, in := range instances {
					if len(tc.exports) != 0 {
						got, err := in.GlobalValue(fmt.Sprintf("g%d", tc.exports[0]))
						if err != nil || !got.ExternRef().IsNull() {
							t.Fatal("externref directory did not outlive compiled handle", got, err)
						}
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
				rv64CheckStateOnlyCode(t, c, 0)
			})
		}
	}
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		if c, err := Compile(nil, rv64StateOnlyExternrefGlobalDirectorySource(257)); c != nil || err == nil {
			if c != nil {
				_ = c.Close()
			}
			t.Fatal("257 externref globals admitted", err)
		}
		exports := make([][]byte, 257)
		for i := range exports {
			exports[i] = wasmtest.ExportEntry(fmt.Sprintf("e%d", i), byte(wasm.ExternGlobal), 0)
		}
		source := wasmtest.Module(
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(exports...)),
		)
		if c, err := Compile(nil, source); c != nil || err == nil {
			if c != nil {
				_ = c.Close()
			}
			t.Fatal("257 externref exports admitted", err)
		}
	}
}

func TestRV64PublicStateOnlyExternrefGlobalDirectoryArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-externref-directory-admission", rv64StateOnlyExternrefGlobalDirectorySource(2, 0, 1), true)
	defer c.Close()
	for name, mutate := range map[string]func(*Compiled){
		"second-type":        func(v *Compiled) { v.Globals[1].Type = ValFuncRef },
		"second-mutable":     func(v *Compiled) { v.Globals[1].Mutable = true },
		"second-bits":        func(v *Compiled) { v.Globals[1].Bits = 1 },
		"second-init-global": func(v *Compiled) { v.Globals[1].HasInitGlobal = true },
		"second-init-func":   func(v *Compiled) { v.Globals[1].HasInitFunc = true },
		"second-value-index": func(v *Compiled) { v.Globals[1].ValueTypeIndex = 1 },
		"export-range":       func(v *Compiled) { v.GlobalExports["g1"] = 2 },
		"export-257": func(v *Compiled) {
			v.GlobalExports = make(map[string]int, 257)
			for i := range 257 {
				v.GlobalExports[fmt.Sprintf("e%d", i)] = 0
			}
		},
		"global-257": func(v *Compiled) {
			for len(v.Globals) < 257 {
				v.Globals = append(v.Globals, v.Globals[0])
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Globals = append([]GlobalDef(nil), bad.Globals...)
			bad.ValueTypes = append([]ValueTypeDescriptor(nil), bad.ValueTypes...)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("hostile externref directory admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile externref directory artifact admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalSource(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-externref-global", rv64StateOnlyExternrefGlobalSource(true), artifact)
			if !rv64StateOnlyExternrefGlobals(c) || c.requiredFeatures != CoreFeatureReferenceTypes || compiledStructuralRequiredFeatures(c) != CoreFeatureReferenceTypes {
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
				if !rv64StateOnlyExternrefGlobals(loaded) {
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
