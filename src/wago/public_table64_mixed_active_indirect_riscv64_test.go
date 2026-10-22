//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MixedTable64ActiveIndirectSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 4, 4)
	importedActive := []byte{0x04, 0x42, 1, 0x0b, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	ownedActive := []byte{0x06, 0x01, 0x42, 1, 0x0b, 0x70, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	caller := func(table byte) []byte { return []byte{0x20, 0, 0x11, 0, table, 0x0b} }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call0", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("call1", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(importedActive, ownedActive)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 8, 0x0b}),
			wasmtest.Code(caller(0)),
			wasmtest.Code(caller(1)),
		)),
	)
}

func TestRV64PublicMixedTable64ActiveIndirect(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "mixed-table64-active-indirect", rv64MixedTable64ActiveIndirectSource(), artifact)
			view := code.executionView()
			if !rv64ArtifactContractValid(view) {
				t.Fatal("mixed active indirect metadata contract")
			}
			imported := rv64MixedSizeTestTable(t, true, 4, 4)
			defer imported.Close()
			in, err := Instantiate(code, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			owned, err := in.ExportedTable("table1")
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			beforeImported, beforeOwned := slices.Clone(imported.desc), slices.Clone(owned.desc)
			for _, tc := range []struct {
				name  string
				index uint64
				want  uint64
			}{{"call0", 1, 7}, {"call0", 2, 8}, {"call1", 1, 7}, {"call1", 2, 8}} {
				if got, invokeErr := in.Invoke(tc.name, tc.index); invokeErr != nil || !slices.Equal(got, []uint64{tc.want}) {
					t.Fatal("mixed active indirect result", tc, got, invokeErr)
				}
			}
			if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(owned.desc, beforeOwned) {
				t.Fatal("call_indirect mutated active tables")
			}
			for _, tc := range []struct {
				name  string
				index uint64
			}{{"call0", 0}, {"call1", 3}, {"call0", 1 << 32}, {"call1", ^uint64(0)}} {
				got, invokeErr := in.Invoke(tc.name, tc.index)
				var trap *coreruntime.TrapError
				if invokeErr == nil || got != nil || !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("mixed active indirect null/high row did not trap", tc, got, invokeErr)
				}
			}
		})
	}
}

func TestRV64PublicMixedTable64ActiveIndirectLifetimes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			readerCode := rv64TableStageCompile(t, "mixed-active-indirect-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer readerCode.Close()
			t.Run("imported", func(t *testing.T) {
				code := rv64TableStageCompile(t, "mixed-active-indirect-imported", rv64MixedTable64ActiveIndirectSource(), artifact)
				imported := rv64MixedSizeTestTable(t, true, 4, 4)
				defer imported.Close()
				in, err := Instantiate(code, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				owned, err := in.ExportedTable("table1")
				if err != nil {
					t.Fatal(err)
				}
				if err := owned.Close(); err != nil {
					t.Fatal("close owned table before imported lifetime check", err)
				}
				if err := code.Close(); err != nil {
					t.Fatal("close producer code before imported lifetime check", err)
				}
				if err := in.Close(); err != nil {
					t.Fatal("close producer before imported lifetime check", err)
				}
				reader, err := Instantiate(readerCode, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				for index, want := range map[uint64]uint64{1: 7, 2: 8} {
					if got, invokeErr := reader.Invoke("call", index); invokeErr != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal("late imported reader", index, got, invokeErr)
					}
				}
			})
			t.Run("owned", func(t *testing.T) {
				code := rv64TableStageCompile(t, "mixed-active-indirect-owned", rv64MixedTable64ActiveIndirectSource(), artifact)
				imported := rv64MixedSizeTestTable(t, true, 4, 4)
				defer imported.Close()
				in, err := Instantiate(code, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				owned, err := in.ExportedTable("table1")
				if err != nil {
					t.Fatal(err)
				}
				defer owned.Close()
				reader, err := Instantiate(readerCode, NewImports().Table("env", "table", owned))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				nullerCode := rv64TableStageCompile(t, "mixed-active-indirect-nuller", rv64ImportedTable64GetSetInstructionSource(), artifact)
				nuller, err := Instantiate(nullerCode, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				for _, index := range []uint64{1, 2} {
					if got, invokeErr := nuller.Invoke("roundtrip", index); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("clear imported row", index, got, invokeErr)
					}
					isNull, nullErr := imported.EntryIsNull(index)
					if nullErr != nil || !isNull {
						t.Fatal("imported row remained live", index, isNull, nullErr)
					}
				}
				if err := nuller.Close(); err != nil {
					t.Fatal("close imported-table nuller", err)
				}
				if err := nullerCode.Close(); err != nil {
					t.Fatal("close nuller code", err)
				}
				if err := code.Close(); err != nil {
					t.Fatal("close producer code before owned lifetime check", err)
				}
				if err := in.Close(); err != nil {
					t.Fatal("close producer before owned lifetime check", err)
				}
				for index, want := range map[uint64]uint64{1: 7, 2: 8} {
					if got, invokeErr := reader.Invoke("call", index); invokeErr != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal("retained owned reader", index, got, invokeErr)
					}
				}
			})
		})
	}
}

func TestRV64PublicMixedTable64ActiveIndirectGates(t *testing.T) {
	base := rv64MixedTable64ActiveIndirectSource()
	for name, source := range map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 4, 4}, []byte{0x70, 0x01, 4, 4}, 1),
		"wrong-table":    bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x11, 0, 1}, []byte{0x20, 0, 0x11, 0, 0}, 1),
		"target-order":   bytes.Replace(bytes.Clone(base), []byte{0xd2, 0, 0x0b, 0xd2, 1}, []byte{0xd2, 1, 0x0b, 0xd2, 0}, 1),
		"target-body":    bytes.Replace(bytes.Clone(base), []byte{0x41, 8, 0x0b}, []byte{0x41, 9, 0x0b}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name != "import-table32" {
				if err != nil {
					t.Fatal("valid table variant", err)
				}
				defer c.Close()
				rv64RequireTableArtifactMetadata(t, c)
			} else {
				if c != nil {
					c.Close()
				}
				if err == nil {
					t.Fatal("invalid table index width admitted")
				}
			}
		})
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("import-artifact%t", artifact), func(t *testing.T) {
			compiled := rv64TableStageCompile(t, "mixed-active-indirect-import-gate", base, artifact)
			defer compiled.Close()
			for _, tc := range []struct {
				name  string
				table *Table
			}{
				{"table32", rv64MixedSizeTestTable(t, false, 4, 4)},
				{"small", rv64MixedSizeTestTable(t, true, 3, 4)},
				{"large-max", rv64MixedSizeTestTable(t, true, 4, 5)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					defer tc.table.Close()
					before := slices.Clone(tc.table.desc)
					in, err := Instantiate(compiled, NewImports().Table("env", "table", tc.table))
					if in != nil {
						in.Close()
					}
					if err == nil || !slices.Equal(tc.table.desc, before) {
						t.Fatal("incompatible mixed active-indirect import admitted or mutated")
					}
				})
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"feature":       func(c *Compiled) { c.requiredFeatures |= CoreFeatureBulkMemoryOperations },
		"reference-abi": func(c *Compiled) { c.nativeReferenceCallABI = true },
		"type-key":      func(c *Compiled) { c.FuncTypeID[3] ^= 1 },
		"entry":         func(c *Compiled) { c.Entry[3]-- },
		"internal-entry": func(c *Compiled) {
			c.InternalEntry[3]--
		},
		"element":      func(c *Compiled) { c.Elems[1].Values[1].FuncIndex = 0 },
		"table-export": func(c *Compiled) { c.tableExports["table1"] = 0 },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			compiled := rv64TableStageCompile(t, "mixed-active-indirect-gate", base, false)
			defer compiled.Close()
			bad := rv64ReferenceImportTailCopy(compiled)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.Elems = slices.Clone(bad.Elems)
			for i := range bad.Elems {
				bad.Elems[i].Offset.Expr = bytes.Clone(bad.Elems[i].Offset.Expr)
				bad.Elems[i].Values = slices.Clone(bad.Elems[i].Values)
			}
			mutate(bad)
			if bad.validate() == nil {
				t.Fatal("hostile mixed active-indirect metadata admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile mixed active-indirect artifact admitted")
			}
		})
	}
}
