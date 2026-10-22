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

func rv64MixedTable64PassiveVectorSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 4, 4)
	passive := []byte{0x05, 0x70, 0x02, 0xd2, 0, 0x0b, 0xd2, 1, 0x0b}
	init := func(table byte) []byte {
		return []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, table, 0x41, 1, 0x0b}
	}
	call := func(table byte) []byte { return []byte{0x20, 0, 0x11, 1, table, 0x0b} }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(0), wasmtest.ULEB(0),
			wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2),
		)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init0", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("init1", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("drop", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("call0", byte(wasm.ExternFunc), 5),
			wasmtest.ExportEntry("call1", byte(wasm.ExternFunc), 6),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(passive)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 8, 0x0b}),
			wasmtest.Code(init(0)),
			wasmtest.Code(init(1)),
			wasmtest.Code([]byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}),
			wasmtest.Code(call(0)),
			wasmtest.Code(call(1)),
		)),
	)
}

func TestRV64PublicMixedTable64PassiveVector(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "mixed-table64-passive-vector", rv64MixedTable64PassiveVectorSource(), artifact)
			defer code.Close()
			view := code.executionView()
			if !rv64ArtifactContractValid(view) {
				t.Fatal("mixed passive vector metadata contract")
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

			for _, name := range []string{"call0", "call1"} {
				if got, invokeErr := in.Invoke(name, 1); invokeErr == nil || got != nil {
					t.Fatal("uninitialized mixed passive row called", name, got, invokeErr)
				}
			}

			beforeOwned := slices.Clone(owned.desc)
			if got, invokeErr := in.Invoke("init0", 1, 0xfeedface00000000, 0xdeadbeef00000002); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("initialize imported passive vector", got, invokeErr)
			}
			if !slices.Equal(owned.desc, beforeOwned) {
				t.Fatal("imported passive initialization changed owned table")
			}
			beforeImported := slices.Clone(imported.desc)
			if got, invokeErr := in.Invoke("init1", 1, 0x1234567800000000, 0xffffffff00000002); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("initialize owned passive vector", got, invokeErr)
			}
			if !slices.Equal(imported.desc, beforeImported) {
				t.Fatal("owned passive initialization changed imported table")
			}
			for _, tc := range []struct {
				name  string
				index uint64
				want  uint64
			}{{"call0", 1, 7}, {"call0", 2, 8}, {"call1", 1, 7}, {"call1", 2, 8}} {
				if got, invokeErr := in.Invoke(tc.name, tc.index); invokeErr != nil || !slices.Equal(got, []uint64{tc.want}) {
					t.Fatal("mixed passive target", tc, got, invokeErr)
				}
			}

			if got, invokeErr := in.Invoke("init1", 0, 0xabcdef0100000001, 0x0123456700000001); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("partial passive vector source", got, invokeErr)
			}
			if got, invokeErr := in.Invoke("call1", 0); invokeErr != nil || !slices.Equal(got, []uint64{8}) {
				t.Fatal("partial passive vector identity", got, invokeErr)
			}

			beforeImported, beforeOwned = slices.Clone(imported.desc), slices.Clone(owned.desc)
			for _, tc := range []struct {
				name                string
				function            uint32
				destination, source uint64
				count               uint64
			}{{"init0", 2, 1 << 32, 0, 0}, {"init0", 2, 3, 0, 2}, {"init0", 2, 0, 1, 2}, {"init1", 3, ^uint64(0), 0, 0}, {"init1", 3, 4, 0, 1}, {"init1", 3, 0, 2, 1}} {
				got, invokeErr := in.Invoke(tc.name, tc.destination, tc.source, tc.count)
				var trap *coreruntime.TrapError
				if invokeErr == nil || got != nil || !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function {
					t.Fatal("mixed passive range did not trap", tc, got, invokeErr)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(owned.desc, beforeOwned) {
					t.Fatal("mixed passive trap changed storage", tc)
				}
			}

			for attempt := 0; attempt < 2; attempt++ {
				if got, invokeErr := in.Invoke("drop"); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("mixed passive elem.drop", attempt, got, invokeErr)
				}
			}
			for _, name := range []string{"init0", "init1"} {
				if got, invokeErr := in.Invoke(name, 4, 0, 0); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("zero-length initialization after drop", name, got, invokeErr)
				}
				got, invokeErr := in.Invoke(name, 0, 0, 1)
				var trap *coreruntime.TrapError
				if invokeErr == nil || got != nil || !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds {
					t.Fatal("nonzero initialization after drop", name, got, invokeErr)
				}
			}
		})
	}
}

func TestRV64PublicMixedTable64PassiveVectorLifetimes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			readerCode := rv64TableStageCompile(t, "mixed-passive-vector-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer readerCode.Close()
			t.Run("imported", func(t *testing.T) {
				code := rv64TableStageCompile(t, "mixed-passive-vector-imported", rv64MixedTable64PassiveVectorSource(), artifact)
				imported := rv64MixedSizeTestTable(t, true, 4, 4)
				defer imported.Close()
				in, err := Instantiate(code, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				if got, invokeErr := in.Invoke("init0", 1, 0, 2); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("publish imported passive vector", got, invokeErr)
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
						t.Fatal("late imported passive reader", index, got, invokeErr)
					}
				}
			})
			t.Run("owned", func(t *testing.T) {
				code := rv64TableStageCompile(t, "mixed-passive-vector-owned", rv64MixedTable64PassiveVectorSource(), artifact)
				imported := rv64MixedSizeTestTable(t, true, 4, 4)
				defer imported.Close()
				in, err := Instantiate(code, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"init0", "init1"} {
					if got, invokeErr := in.Invoke(name, 1, 0, 2); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("publish passive vector", name, got, invokeErr)
					}
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
				nullerCode := rv64TableStageCompile(t, "mixed-passive-vector-nuller", rv64ImportedTable64GetSetInstructionSource(), artifact)
				nuller, err := Instantiate(nullerCode, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				for _, index := range []uint64{1, 2} {
					if got, invokeErr := nuller.Invoke("roundtrip", index); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("clear imported passive row", index, got, invokeErr)
					}
					isNull, nullErr := imported.EntryIsNull(index)
					if nullErr != nil || !isNull {
						t.Fatal("imported passive row remained live", index, isNull, nullErr)
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
						t.Fatal("retained owned passive reader", index, got, invokeErr)
					}
				}
			})
		})
	}
}

func TestRV64PublicMixedTable64PassiveVectorGates(t *testing.T) {
	base := rv64MixedTable64PassiveVectorSource()
	for name, source := range map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 4, 4}, []byte{0x70, 0x01, 4, 4}, 1),
		"wrong-init":     bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0c, 0, 1}, []byte{0xfc, 0x0c, 0, 0}, 1),
		"wrong-call":     bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x11, 1, 1}, []byte{0x20, 0, 0x11, 1, 0}, 1),
		"target-order":   bytes.Replace(bytes.Clone(base), []byte{0xd2, 0, 0x0b, 0xd2, 1}, []byte{0xd2, 1, 0x0b, 0xd2, 0}, 1),
		"target-body":    bytes.Replace(bytes.Clone(base), []byte{0x41, 8, 0x0b}, []byte{0x41, 9, 0x0b}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name != "import-table32" {
				if err != nil {
					t.Fatal("valid mixed table source", err)
				}
				defer c.Close()
				rv64RequireTableArtifactMetadata(t, c)
			} else {
				if c != nil {
					c.Close()
				}
				if err == nil {
					t.Fatal("invalid table width or malformed source admitted")
				}
			}
		})
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("import-artifact%t", artifact), func(t *testing.T) {
			compiled := rv64TableStageCompile(t, "mixed-passive-vector-import-gate", base, artifact)
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
						t.Fatal("incompatible mixed passive-vector import admitted or mutated")
					}
				})
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"feature":       func(c *Compiled) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"reference-abi": func(c *Compiled) { c.nativeReferenceCallABI = true },
		"type-key":      func(c *Compiled) { c.FuncTypeID[6] ^= 1 },
		"entry":         func(c *Compiled) { c.Entry[6]-- },
		"internal-entry": func(c *Compiled) {
			c.InternalEntry[6]--
		},
		"element":      func(c *Compiled) { c.passiveElems[0].Values[1].FuncIndex = 0 },
		"table-export": func(c *Compiled) { c.tableExports["table1"] = 0 },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			compiled := rv64TableStageCompile(t, "mixed-passive-vector-gate", base, false)
			defer compiled.Close()
			bad := rv64ReferenceImportTailCopy(compiled)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.passiveElems = slices.Clone(bad.passiveElems)
			bad.passiveElems[0].Values = slices.Clone(bad.passiveElems[0].Values)
			mutate(bad)
			if bad.validate() == nil {
				t.Fatal("hostile mixed passive-vector metadata admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if name == "feature" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
					t.Fatal("serialization did not restore structural passive-vector features", err)
				}
				return
			}
			if err == nil {
				t.Fatal("hostile mixed passive-vector artifact admitted")
			}
		})
	}
}
