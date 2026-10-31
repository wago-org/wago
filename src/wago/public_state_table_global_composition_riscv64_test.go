//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"os"
	"strings"
	"testing"

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
			if c.Globals[0].Bits>>32 != 0 || c.Globals[2].Bits>>32 != 0 {
				t.Fatal("narrow scalar literals were not canonical", c.Globals[0].Bits, c.Globals[2].Bits)
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
		"cross-kind-export-name": rv64ReplaceTableStageSection(t, base, 7, exports(
			wasmtest.ExportEntry("i32", 1, 0),
			wasmtest.ExportEntry("i32", 3, 0),
			wasmtest.ExportEntry("i64", 3, 1),
			wasmtest.ExportEntry("f32", 3, 2),
			wasmtest.ExportEntry("f64", 3, 3),
		)),
	} {
		t.Run(name, func(t *testing.T) {
			rv64RequireCore3SourceValidation(t, source)
		})
	}
}
