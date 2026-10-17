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

func rv64MixedTable64IndirectSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	declaredTarget := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	caller := func(index byte) []byte {
		return []byte{0x20, 0, 0xd2, 0, 0x26, index, 0x20, 0, 0x11, 0, index, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call0", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("call1", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(declaredTarget)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code(caller(0)),
			wasmtest.Code(caller(1)),
		)),
	)
}

func TestRV64PublicMixedTable64Indirect(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			writerCode := rv64TableStageCompile(t, "mixed-table64-indirect-writer", rv64MixedTable64IndirectSource(), artifact)
			readerCode := rv64TableStageCompile(t, "mixed-table64-indirect-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer readerCode.Close()
			view := writerCode.executionView()
			if view.nativeTable64ABI != nativeTable64NoABI || !rv64ArtifactContractValid(view) || view.tableCount() != 2 || view.tableImportCount() != 1 || view.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || !view.NeedsFuncRefDescs {
				t.Fatal("mixed table64 indirect metadata contract")
			}
			imported := rv64MixedSizeTestTable(t, true, 2, 4)
			defer imported.Close()
			writer, err := Instantiate(writerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			alias, err := writer.ExportedTable("table0")
			if err != nil || alias != imported {
				t.Fatal("mixed indirect imported identity", err)
			}
			local, err := writer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 3 {
				t.Fatal("mixed indirect local identity", local, err)
			}
			defer local.Close()
			for _, tc := range []struct {
				name  string
				index uint64
				other *Table
			}{{"call1", 0, imported}, {"call1", 2, imported}, {"call0", 0, local}, {"call0", 1, local}} {
				other := slices.Clone(tc.other.desc)
				if got, err := writer.Invoke(tc.name, tc.index); err != nil || !slices.Equal(got, []uint64{7}) || !slices.Equal(tc.other.desc, other) {
					t.Fatal("mixed table64 indirect call/isolation", tc, got, err)
				}
			}
			beforeImported, beforeLocal := slices.Clone(imported.desc), slices.Clone(local.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				index    uint64
			}{{"call0", 1, 2}, {"call1", 2, 3}, {"call0", 1, 1 << 32}, {"call1", 2, ^uint64(0)}} {
				got, err := writer.Invoke(tc.name, tc.index)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
					t.Fatal("mixed table64 indirect index did not trap", tc, got, err)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed table64 indirect trap changed storage", tc)
				}
			}
			localReader, err := Instantiate(readerCode, NewImports().Table("env", "table", local))
			if err != nil {
				t.Fatal("mixed indirect local reader attachment", err)
			}
			defer localReader.Close()
			if err := writerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := localReader.Invoke("call", 2); err != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("mixed indirect retained local target", got, err)
			}
			importedReader, err := Instantiate(readerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal("mixed indirect imported reader after close", err)
			}
			got, callErr := importedReader.Invoke("call", 0)
			closeErr := importedReader.Close()
			if callErr != nil || closeErr != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("mixed indirect retained imported target", got, callErr, closeErr)
			}
		})
	}
}

func TestRV64PublicMixedTable64IndirectImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-indirect-import-gates", rv64MixedTable64IndirectSource(), artifact)
			defer c.Close()
			for _, tc := range []struct {
				name  string
				table *Table
			}{
				{"table32", rv64MixedSizeTestTable(t, false, 1, 4)},
				{"small", rv64MixedSizeTestTable(t, true, 0, 4)},
				{"large-max", rv64MixedSizeTestTable(t, true, 1, 5)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					defer tc.table.Close()
					before := slices.Clone(tc.table.desc)
					in, err := Instantiate(c, NewImports().Table("env", "table", tc.table))
					if in != nil {
						in.Close()
					}
					if err == nil || !slices.Equal(tc.table.desc, before) {
						t.Fatal("incompatible mixed indirect import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64IndirectSourceGates(t *testing.T) {
	base := rv64MixedTable64IndirectSource()
	for name, source := range map[string][]byte{
		"import-table32":   bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":    bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
		"wrong-table":      bytes.Replace(bytes.Clone(base), []byte{0x26, 1, 0x20, 0, 0x11, 0, 1}, []byte{0x26, 0, 0x20, 0, 0x11, 0, 1}, 1),
		"element-encoding": bytes.Replace(bytes.Clone(base), []byte{0x03, 0x00, 0x01, 0x00}, []byte{0x00, 0x41, 0x00, 0x0b}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name == "wrong-table" {
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
}

func TestRV64PublicMixedTable64IndirectArtifactGates(t *testing.T) {
	mutations := map[string]func(*Compiled){
		"witness":        func(c *Compiled) { c.nativeTable64ABI = nativeTable64ImportedSizeABI },
		"missing-ref":    func(c *Compiled) { c.requiredFeatures &^= CoreFeatureReferenceTypes },
		"missing-bulk":   func(c *Compiled) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"reference-abi":  func(c *Compiled) { c.nativeReferenceCallABI = true },
		"descriptor":     func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"type-key":       func(c *Compiled) { c.FuncTypeID = c.FuncTypeID[:2] },
		"wrong-key":      func(c *Compiled) { c.FuncTypeID[2] ^= 1 },
		"import-table32": func(c *Compiled) { c.TableAddr64 = false },
		"import-limit":   func(c *Compiled) { c.tableImportMax = 0 },
		"local-table32":  func(c *Compiled) { c.extraTables[0].Addr64 = false },
		"local-import":   func(c *Compiled) { c.extraTables[0].ImportKey = "env.other" },
		"signature":      func(c *Compiled) { c.Funcs[2].Params[0] = ValI32 },
		"function-export": func(c *Compiled) {
			c.Exports["call1"] = 1
		},
		"table-export":   func(c *Compiled) { c.tableExports["table1"] = 0 },
		"active-element": func(c *Compiled) { c.Elems = []ElemInit{{Mode: ElemModeActive, TableIndex: 1, RefType: ValFuncRef}} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			base := rv64TableStageCompile(t, "mixed-table64-indirect-gates", rv64MixedTable64IndirectSource(), false)
			defer base.Close()
			bad := rv64ReferenceImportTailCopy(base)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[2].Params = slices.Clone(bad.Funcs[2].Params)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			mutate(bad)
			if bad.validate() == nil {
				t.Fatal("hostile mixed table64 indirect metadata admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if name == "missing-ref" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
					t.Fatal("serialization did not restore structural mixed indirect features", err)
				}
				return
			}
			if err == nil {
				t.Fatal("hostile mixed table64 indirect artifact admitted")
			}
		})
	}
}
