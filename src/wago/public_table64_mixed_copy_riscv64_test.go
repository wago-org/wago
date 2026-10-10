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

func rv64MixedTable64CopySource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	body := func(index byte) []byte {
		return []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, index, index, 0x41, 1, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("copy1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(0)), wasmtest.Code(body(1)))),
	)
}

func TestRV64PublicMixedTable64Copy(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-copy-producer", rv64Table64IndirectInstructionSource(), artifact)
			nullerCode := rv64TableStageCompile(t, "mixed-table64-copy-nuller", rv64ImportedTable64GetSetInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "mixed-table64-copy-consumer", rv64MixedTable64CopySource(), artifact)
			defer consumerCode.Close()
			view := consumerCode.executionView()
			if view.nativeTable64ABI != nativeTable64ImportedCopyABI || !rv64MixedTable64Copy(view) || view.tableCount() != 2 || view.tableImportCount() != 1 || view.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || !view.NeedsFuncRefDescs {
				t.Fatal("mixed table64.copy metadata contract")
			}
			producer, err := Instantiate(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			imported, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			defer imported.Close()
			for index := uint64(0); index < 2; index++ {
				if got, err := producer.Invoke("call", index); err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("seed mixed imported copy row", index, got, err)
				}
			}
			imports := NewImports().Table("env", "table", imported)
			nuller, err := Instantiate(nullerCode, imports)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := Instantiate(consumerCode, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			alias, err := consumer.ExportedTable("table0")
			if err != nil || alias != imported {
				t.Fatal("mixed copy imported table identity", err)
			}
			local, err := consumer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 3 {
				t.Fatal("mixed copy owned table", local, err)
			}
			defer local.Close()

			if got, err := nuller.Invoke("roundtrip", 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed copy pattern setup", got, err)
			}
			beforeLocal := slices.Clone(local.desc)
			expected := slices.Clone(imported.desc)
			copy(expected[8+coreruntime.TableEntryBytes:8+2*coreruntime.TableEntryBytes], expected[8:8+coreruntime.TableEntryBytes])
			if got, err := consumer.Invoke("copy0", 1, 0, 1); err != nil || !slices.Equal(got, []uint64{1}) || !slices.Equal(imported.desc, expected) {
				t.Fatal("mixed imported copy differs from row oracle", got, err)
			}
			if !slices.Equal(local.desc, beforeLocal) {
				t.Fatal("mixed imported copy changed local descriptor")
			}
			beforeImported := slices.Clone(imported.desc)
			if got, err := consumer.Invoke("copy1", 2, 2, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed owned copy selected wrong descriptor", got, err)
			}
			if !slices.Equal(imported.desc, beforeImported) {
				t.Fatal("mixed owned copy changed imported descriptor")
			}
			for _, call := range []struct {
				name  string
				index uint64
			}{{"copy0", 2}, {"copy1", 3}} {
				if got, err := consumer.Invoke(call.name, call.index, call.index, 0); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("mixed copy exact-end zero range", call, got, err)
				}
			}

			beforeImported, beforeLocal = slices.Clone(imported.desc), slices.Clone(local.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				dest     uint64
				source   uint64
				count    uint64
			}{
				{"copy0", 0, 1, 0, 2}, {"copy0", 0, 0, 1, 2}, {"copy0", 0, 1 << 32, 0, 0}, {"copy0", 0, 0, 1 << 32, 0}, {"copy0", 0, 0, 0, 1 << 32},
				{"copy1", 1, 2, 0, 2}, {"copy1", 1, 0, 2, 2}, {"copy1", 1, ^uint64(0), 0, 2}, {"copy1", 1, 0, ^uint64(0), 2}, {"copy1", 1, 0, 0, ^uint64(0)},
			} {
				got, err := consumer.Invoke(tc.name, tc.dest, tc.source, tc.count)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
					t.Fatal("mixed table64.copy range did not trap", tc, got, err)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed table64.copy trap changed storage", tc)
				}
			}

			if got, err := nuller.Invoke("roundtrip", 0); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed copy retained pattern setup", got, err)
			}
			if err := nuller.Close(); err != nil {
				t.Fatal(err)
			}
			if err := nullerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("copy0", 0, 1, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed imported copy after producer close", got, err)
			}
			if null, err := imported.EntryIsNull(0); err != nil || null {
				t.Fatal("mixed imported retained copy remained null", null, err)
			}
		})
	}
}

func TestRV64PublicMixedTable64CopyImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-copy-import-gates", rv64MixedTable64CopySource(), artifact)
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
						t.Fatal("incompatible mixed table64 copy import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64CopySourceGates(t *testing.T) {
	base := rv64MixedTable64CopySource()
	mutations := map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
		"wrong-dest":     bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0e, 1, 1}, []byte{0xfc, 0x0e, 0, 1}, 1),
		"wrong-source":   bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0e, 1, 1}, []byte{0xfc, 0x0e, 1, 0}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("nearby mixed table64.copy source admitted")
			}
		})
	}
}

func TestRV64PublicMixedTable64CopyArtifactGates(t *testing.T) {
	mutations := map[string]func(*Compiled){
		"witness":        func(c *Compiled) { c.nativeTable64ABI = nativeTable64CopyABI },
		"feature":        func(c *Compiled) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"reference-abi":  func(c *Compiled) { c.nativeReferenceCallABI = true },
		"descriptor":     func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"import-table32": func(c *Compiled) { c.TableAddr64 = false },
		"import-limit":   func(c *Compiled) { c.tableImportMax = 0 },
		"local-table32":  func(c *Compiled) { c.extraTables[0].Addr64 = false },
		"local-import":   func(c *Compiled) { c.extraTables[0].ImportKey = "env.other" },
		"signature":      func(c *Compiled) { c.Funcs[1].Params[2] = ValI32 },
		"function-export": func(c *Compiled) {
			for name, index := range c.Exports {
				if index == 1 {
					c.Exports[name] = 0
					return
				}
			}
		},
		"table-export": func(c *Compiled) { c.tableExports["table1"] = 0 },
		"element": func(c *Compiled) {
			c.Elems = []ElemInit{{Mode: ElemModeActive, TableIndex: 1, RefType: ValFuncRef}}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			base := rv64TableStageCompile(t, "mixed-table64-copy-gates", rv64MixedTable64CopySource(), false)
			defer base.Close()
			bad := cloneCompiledMetadata(base.executionView())
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[1].Params = slices.Clone(bad.Funcs[1].Params)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.validateMemo, bad.codeCache = &validateMemo{}, nil
			mutate(bad)
			if rv64MixedTable64Copy(bad) || !errors.Is(railshotValidateCompiled(bad), railshot.ErrUnsupported) {
				t.Fatal("hostile mixed table64.copy metadata admitted")
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
				t.Fatal("hostile mixed table64.copy artifact admitted")
			}
		})
	}
}
