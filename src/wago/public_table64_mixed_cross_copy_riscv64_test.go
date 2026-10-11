//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
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

func rv64MixedTable64CrossCopySource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	body := func(destination, source byte) []byte {
		return []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, destination, source, 0x41, 1, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy01", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("copy10", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(0, 1)), wasmtest.Code(body(1, 0)))),
	)
}

func TestRV64PublicMixedTable64CrossCopy(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			writerCode := rv64TableStageCompile(t, "mixed-table64-cross-copy-writer", rv64MixedTable64IndirectSource(), artifact)
			nullerCode := rv64TableStageCompile(t, "mixed-table64-cross-copy-nuller", rv64ImportedTable64GetSetInstructionSource(), artifact)
			crossCode := rv64TableStageCompile(t, "mixed-table64-cross-copy-consumer", rv64MixedTable64CrossCopySource(), artifact)
			readerCode := rv64TableStageCompile(t, "mixed-table64-cross-copy-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer readerCode.Close()
			view := crossCode.executionView()
			if view.nativeTable64ABI != nativeTable64ImportedCopyABI || !rv64MixedTable64CrossCopy(view) || rv64MixedTable64Copy(view) || view.tableCount() != 2 || view.tableImportCount() != 1 || view.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || !view.NeedsFuncRefDescs {
				t.Fatal("mixed table64 cross-copy metadata contract")
			}

			imported := rv64MixedSizeTestTable(t, true, 2, 4)
			defer imported.Close()
			imports := NewImports().Table("env", "table", imported)
			writer, err := Instantiate(writerCode, imports)
			if err != nil {
				t.Fatal(err)
			}
			cross, err := Instantiate(crossCode, imports)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := writer.Invoke("call0", 0); err != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("seed host-owned imported row", got, err)
			}
			if got, err := cross.Invoke("copy10", 1, 0, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("copy imported row into owned table", got, err)
			}
			owned, err := cross.ExportedTable("table1")
			if err != nil || owned == imported || owned.Size() != 3 {
				t.Fatal("materialize mixed cross-copy owned table after copy", owned, err)
			}
			defer owned.Close()
			if null, err := owned.EntryIsNull(1); err != nil || null {
				t.Fatal("owned destination row", null, err)
			}

			nuller, err := Instantiate(nullerCode, imports)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := nuller.Invoke("roundtrip", 0); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("clear imported source row", got, err)
			}
			if err := nuller.Close(); err != nil {
				t.Fatal(err)
			}
			if err := nullerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if null, err := imported.EntryIsNull(0); err != nil || !null {
				t.Fatal("imported source row remained live", null, err)
			}
			if err := writerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			ownedReader, err := Instantiate(readerCode, NewImports().Table("env", "table", owned))
			if err != nil {
				t.Fatal("attach owned reader after source retirement", err)
			}
			if got, err := ownedReader.Invoke("call", 1); err != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("owned destination retained copied producer", got, err)
			}
			if err := ownedReader.Close(); err != nil {
				t.Fatal(err)
			}

			if got, err := cross.Invoke("copy01", 1, 1, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("copy owned row back into imported table", got, err)
			}
			if got, err := cross.Invoke("copy10", 1, 0, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("clear owned source through imported null row", got, err)
			}
			if null, err := owned.EntryIsNull(1); err != nil || !null {
				t.Fatal("owned source root remained live", null, err)
			}
			if err := crossCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := cross.Close(); err != nil {
				t.Fatal(err)
			}
			lateReader, err := Instantiate(readerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal("attach imported reader after both writers retire", err)
			}
			got, callErr := lateReader.Invoke("call", 1)
			closeErr := lateReader.Close()
			if callErr != nil || closeErr != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("imported destination retained copied producer", got, callErr, closeErr)
			}
		})
	}
}

func TestRV64PublicMixedTable64CrossCopyRanges(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "mixed-table64-cross-copy-ranges", rv64MixedTable64CrossCopySource(), artifact)
			defer code.Close()
			imported := rv64MixedSizeTestTable(t, true, 2, 4)
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
			for _, call := range []struct {
				name      string
				dest, src uint64
			}{{"copy01", 2, 3}, {"copy10", 3, 2}} {
				if got, err := in.Invoke(call.name, call.dest, call.src, 0); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("exact-end zero range", call, got, err)
				}
			}
			beforeImported, beforeOwned := slices.Clone(imported.desc), slices.Clone(owned.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				dest     uint64
				source   uint64
				count    uint64
			}{
				{"copy01", 0, 1, 0, 2}, {"copy01", 0, 0, 3, 1}, {"copy01", 0, 1 << 32, 0, 0}, {"copy01", 0, 0, 1 << 32, 0}, {"copy01", 0, 0, 0, 1 << 32},
				{"copy10", 1, 2, 0, 2}, {"copy10", 1, 0, 1, 2}, {"copy10", 1, ^uint64(0), 0, 1}, {"copy10", 1, 0, ^uint64(0), 1}, {"copy10", 1, 0, 0, ^uint64(0)},
			} {
				got, err := in.Invoke(tc.name, tc.dest, tc.source, tc.count)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
					t.Fatal("mixed cross-copy range did not trap", tc, got, err)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(owned.desc, beforeOwned) {
					t.Fatal("mixed cross-copy trap changed storage", tc)
				}
			}
		})
	}
}

func TestRV64PublicMixedTable64CrossCopyGates(t *testing.T) {
	base := rv64MixedTable64CrossCopySource()
	for name, source := range map[string][]byte{
		"same-first":     bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0e, 0, 1}, []byte{0xfc, 0x0e, 0, 0}, 1),
		"same-second":    bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0e, 1, 0}, []byte{0xfc, 0x0e, 1, 1}, 1),
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if c, err := Compile(nil, source); err == nil {
				c.Close()
				t.Fatal("nearby mixed cross-copy source admitted")
			}
		})
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("import-artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "mixed-table64-cross-copy-import-gates", base, artifact)
			defer code.Close()
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
					in, err := Instantiate(code, NewImports().Table("env", "table", tc.table))
					if in != nil {
						in.Close()
					}
					if err == nil || !slices.Equal(tc.table.desc, before) {
						t.Fatal("incompatible mixed cross-copy import admitted or mutated")
					}
				})
			}
		})
	}

	mutations := map[string]func(*Compiled){
		"witness":       func(c *Compiled) { c.nativeTable64ABI = nativeTable64CopyABI },
		"feature":       func(c *Compiled) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"reference-abi": func(c *Compiled) { c.nativeReferenceCallABI = true },
		"descriptor":    func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"import-limit":  func(c *Compiled) { c.tableImportMax = 0 },
		"local-table32": func(c *Compiled) { c.extraTables[0].Addr64 = false },
		"function":      func(c *Compiled) { c.Exports["copy10"] = 0 },
		"table":         func(c *Compiled) { c.tableExports["table1"] = 0 },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[0] ^= 1
		},
	}
	for name, mutate := range mutations {
		t.Run("artifact-"+name, func(t *testing.T) {
			base := rv64TableStageCompile(t, "mixed-table64-cross-copy-artifact-gates", rv64MixedTable64CrossCopySource(), false)
			defer base.Close()
			bad := cloneCompiledMetadata(base.executionView())
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[1].Params = slices.Clone(bad.Funcs[1].Params)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.validateMemo, bad.codeCache = &validateMemo{}, nil
			mutate(bad)
			if rv64MixedTable64CrossCopy(bad) || !errors.Is(railshotValidateCompiled(bad), railshot.ErrUnsupported) {
				t.Fatal("hostile mixed cross-copy metadata admitted")
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
				t.Fatal("hostile mixed cross-copy artifact admitted")
			}
		})
	}
}
