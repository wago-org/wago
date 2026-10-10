//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64StateOnlyTable32ScalarGlobalsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(4, []byte{1, 0x70, 1, 2, 5}),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x7f, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 0x7e, 0x0b}),
			wasmtest.GlobalEntry(wasm.F32, false, []byte{0x43, 0, 0, 0, 0x80, 0x0b}),
			wasmtest.GlobalEntry(wasm.F64, false, []byte{0x44, 0x42, 0, 0, 0, 0, 0, 0xf8, 0x7f, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table", 1, 0),
			wasmtest.ExportEntry("i32", 3, 0),
			wasmtest.ExportEntry("i64", 3, 1),
			wasmtest.ExportEntry("f32", 3, 2),
			wasmtest.ExportEntry("f64", 3, 3),
		)),
	)
}

func TestRV64PublicStateOnlyTable32ScalarGlobals(t *testing.T) {
	wantBits := []uint64{0xffffffff, 0xfffffffffffffffe, 0x80000000, 0x7ff8000000000042}
	names := []string{"i32", "i64", "f32", "f64"}
	for _, artifact := range []bool{false, true} {
		t.Run("artifact"+map[bool]string{false: "false", true: "true"}[artifact], func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-table32-scalar-globals", rv64StateOnlyTable32ScalarGlobalsSource(), artifact)
			if !rv64StateOnlyTable32ScalarGlobals(c) || c.tableCount() != 1 || len(c.Globals) != 4 || len(c.GlobalExports) != 4 || len(c.tableExports) != 1 || c.requiredFeatures != 0 || compiledStructuralRequiredFeatures(c) != 0 {
				t.Fatalf("table32/scalar-global metadata contract: witness=%v features=%#x structural=%#x globals=%+v value-types=%+v table=%+v table-exports=%v global-exports=%v native=%d refs=%v", rv64StateOnlyTable32ScalarGlobals(c), c.requiredFeatures, compiledStructuralRequiredFeatures(c), c.Globals, c.ValueTypes, c.tableDef(0), c.tableExports, c.GlobalExports, c.nativeTable64ABI, c.NeedsFuncRefDescs)
			}
			rv64CheckStateOnlyCode(t, c, 0)
			instances := make([]*Instance, 0, 2)
			for range 2 {
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				instances = append(instances, in)
				if len(in.globalCells) != 4 || in.tableDescPtr == 0 {
					t.Fatal("state directory", len(in.globalCells), in.tableDescPtr)
				}
				for i, cell := range in.globalCells {
					if cell == nil || len(cell.cell) != 8 {
						t.Fatal("global cell", i)
					}
					for j := 0; j < i; j++ {
						if cell == in.globalCells[j] {
							t.Fatal("aliased global cells", i, j)
						}
					}
					got, err := in.GlobalValue(names[i])
					if err != nil || got.Bits() != wantBits[i] {
						t.Fatal("scalar literal bits", names[i], got, err)
					}
					if err := in.SetGlobalValue(names[i], got); err == nil || !strings.Contains(err.Error(), "immutable") {
						t.Fatal("immutable scalar global admitted a write", names[i], err)
					}
				}
				table, err := in.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				if table.Size() != 2 {
					t.Fatal("table size", table.Size())
				}
				for index := uint64(0); index < 2; index++ {
					if null, err := table.EntryIsNull(index); err != nil || !null {
						t.Fatal("table entry", index, null, err)
					}
				}
				if _, err := table.EntryIsNull(2); err == nil {
					t.Fatal("table bound ignored")
				}
				if err := table.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if instances[0].tableDescPtr == instances[1].tableDescPtr {
				t.Fatal("table descriptors shared between instances")
			}
			for i := range instances[0].globalCells {
				if instances[0].globalCells[i] == instances[1].globalCells[i] {
					t.Fatal("global cells shared between instances", i)
				}
			}
			rv64CheckStateOnlyCode(t, c, 2)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			for _, in := range instances {
				for i, name := range names {
					if got, err := in.GlobalValue(name); err != nil || got.Bits() != wantBits[i] {
						t.Fatal("global after compiled close", name, got, err)
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

func TestRV64PublicStateOnlyTable32ScalarGlobalsCompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		t.Skip("compiler-free gate")
	}
	if c, err := Compile(nil, rv64StateOnlyTable32ScalarGlobalsSource()); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free table32/scalar-global witness used source compilation", c, err)
	}
	c := rv64TableStageCompile(t, "state-only-table32-scalar-globals", rv64StateOnlyTable32ScalarGlobalsSource(), true)
	if !rv64StateOnlyTable32ScalarGlobals(c) {
		t.Fatal("compiler-free table32/scalar-global artifact lost its witness")
	}
}

func TestRV64PublicStateOnlyTable32ScalarGlobalsSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source admission only")
	}
	base := rv64StateOnlyTable32ScalarGlobalsSource()
	globalSection := func(entries ...[]byte) []byte { return wasmtest.Section(6, wasmtest.Vec(entries...)) }
	exports := func(entries ...[]byte) []byte { return wasmtest.Section(7, wasmtest.Vec(entries...)) }
	for name, source := range map[string][]byte{
		"missing-max": rv64ReplaceTableStageSection(t, base, 4, wasmtest.Section(4, []byte{1, 0x70, 0, 2})),
		"oversize":    rv64ReplaceTableStageSection(t, base, 4, wasmtest.Section(4, []byte{1, 0x70, 1, 2, 0x81, 0x08})),
		"mutable": rv64ReplaceTableStageSection(t, base, 6, globalSection(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x7f, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 0x7e, 0x0b}),
			wasmtest.GlobalEntry(wasm.F32, false, []byte{0x43, 0, 0, 0, 0x80, 0x0b}),
			wasmtest.GlobalEntry(wasm.F64, false, []byte{0x44, 0x42, 0, 0, 0, 0, 0, 0xf8, 0x7f, 0x0b}),
		)),
		"derived": rv64ReplaceTableStageSection(t, base, 6, globalSection(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x7f, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 0, 0x0b}),
			wasmtest.GlobalEntry(wasm.F32, false, []byte{0x43, 0, 0, 0, 0x80, 0x0b}),
			wasmtest.GlobalEntry(wasm.F64, false, []byte{0x44, 0x42, 0, 0, 0, 0, 0, 0xf8, 0x7f, 0x0b}),
		)),
		"missing-export": rv64ReplaceTableStageSection(t, base, 7, exports(
			wasmtest.ExportEntry("table", 1, 0),
			wasmtest.ExportEntry("i32", 3, 0),
			wasmtest.ExportEntry("i64", 3, 1),
			wasmtest.ExportEntry("f32", 3, 2),
		)),
		"aliased-export": rv64ReplaceTableStageSection(t, base, 7, exports(
			wasmtest.ExportEntry("table", 1, 0),
			wasmtest.ExportEntry("i32", 3, 0),
			wasmtest.ExportEntry("i64", 3, 1),
			wasmtest.ExportEntry("f32", 3, 2),
			wasmtest.ExportEntry("f64", 3, 2),
		)),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if c != nil {
				_ = c.Close()
			}
			if err == nil {
				t.Fatal("unsupported table32/scalar-global source admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyTable32ScalarGlobalsArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-table32-scalar-globals-admission", rv64StateOnlyTable32ScalarGlobalsSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"extra-feature":     func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
		"mutable":           func(v *Compiled) { v.Globals[0].Mutable = true },
		"wrong-global-type": func(v *Compiled) { v.Globals[0].Type = ValI64 },
		"wrong-global-pool": func(v *Compiled) { v.Globals[0].ValueTypeIndex = 1 },
		"derived-global":    func(v *Compiled) { v.Globals[1].HasInitGlobal = true; v.Globals[1].InitGlobal = 0 },
		"global-expression": func(v *Compiled) { v.Globals[0].InitExpr = []byte{0x41, 0, 0x0b} },
		"vector-payload":    func(v *Compiled) { v.Globals[0].V128[0] = 1 },
		"missing-global-export": func(v *Compiled) {
			delete(v.GlobalExports, "f64")
		},
		"aliased-global-export": func(v *Compiled) { v.GlobalExports["f64"] = 2 },
		"missing-table-export":  func(v *Compiled) { delete(v.tableExports, "table") },
		"wrong-table-export":    func(v *Compiled) { v.tableExports["table"] = 1 },
		"table64":               func(v *Compiled) { v.TableAddr64 = true },
		"missing-table-max":     func(v *Compiled) { v.TableHasMax = false },
		"oversize-table":        func(v *Compiled) { v.TableMax = railshot.MaxOwnedTableEntries + 1 },
		"table-initializer":     func(v *Compiled) { v.HasTableInitFunc = true; v.TableInitFunc = 1 },
		"element":               func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"function":              func(v *Compiled) { v.Funcs = []FuncSig{{}} },
		"function-export":       func(v *Compiled) { v.Exports["f"] = 0 },
		"start":                 func(v *Compiled) { v.HasStart = true },
		"memory": func(v *Compiled) {
			v.HasMemory = true
			v.MemMinPages = 1
			v.MemMaxPages = 1
		},
		"extra-value-type": func(v *Compiled) {
			v.ValueTypes = append(v.ValueTypes, ValueTypeDescriptor{Kind: ValueTypeI32})
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Globals = slices.Clone(bad.Globals)
			bad.ValueTypes = slices.Clone(bad.ValueTypes)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.Exports = maps.Clone(bad.Exports)
			bad.Elems = slices.Clone(bad.Elems)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table32/scalar-global metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "vector-payload" {
				if err != nil || loaded == nil || loaded.Globals[0].V128 != (V128{}) || !rv64StateOnlyTable32ScalarGlobals(loaded) {
					t.Fatal("irrelevant vector payload was not normalized", loaded, err)
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid table32/scalar-global artifact admitted")
			}
		})
	}
	if c.codeCache.refs != 0 || c.codeCache.mem != nil || c.codeCache.base != 0 || c.codeCache.sealed {
		t.Fatal("table32/scalar-global gates reached executable publication")
	}
}
