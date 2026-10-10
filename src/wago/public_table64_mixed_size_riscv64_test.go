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
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MixedTable64SizeSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("size1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x10, 1, 0x0b}),
		)),
	)
}

func rv64MixedSizeTestTable(t *testing.T, addr64 bool, min, max uint32) *Table {
	t.Helper()
	var table *Table
	var err error
	if addr64 {
		table, err = NewTable64(min, max)
	} else {
		table, err = NewTable(min, max)
	}
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestRV64PublicMixedTable64Size(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-size-producer", rv64Table64GrowInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "mixed-table64-size-consumer", rv64MixedTable64SizeSource(), artifact)
			defer consumerCode.Close()
			view := consumerCode.executionView()
			if view.nativeTable64ABI != nativeTable64ImportedSizeABI || !rv64MixedTable64Size(view) || view.tableCount() != 2 || view.tableImportCount() != 1 || view.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes || !view.NeedsFuncRefDescs {
				t.Fatal("mixed table64.size metadata contract")
			}
			producer, err := Instantiate(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = producer.Close() })
			imported, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			defer imported.Close()
			consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			alias, err := consumer.ExportedTable("table0")
			if err != nil || alias != imported {
				t.Fatal("mixed imported table identity", err)
			}
			local, err := consumer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 2 {
				t.Fatal("mixed owned table identity", local, err)
			}
			defer local.Close()
			check := func(importedSize uint64) {
				t.Helper()
				if got, err := consumer.Invoke("size0"); err != nil || !slices.Equal(got, []uint64{importedSize}) {
					t.Fatal("mixed imported table64.size", got, err)
				}
				if got, err := consumer.Invoke("size1"); err != nil || !slices.Equal(got, []uint64{2}) {
					t.Fatal("mixed owned table64.size", got, err)
				}
			}
			check(2)
			if got, err := producer.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{2}) {
				t.Fatal("mixed provider grow", got, err)
			}
			check(3)
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			check(3)
		})
	}
}

func TestRV64PublicMixedTable64SizeSourceGates(t *testing.T) {
	base := rv64MixedTable64SizeSource()
	mutations := map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 2, 3}, []byte{0x70, 0x01, 2, 3}, 1),
		"wrong-index":    bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x10, 1, 0x0b}, []byte{0xfc, 0x10, 0, 0x0b}, 1),
		"wrong-body":     bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x10, 1, 0x0b}, []byte{0x42, 2, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("nearby mixed table64.size source admitted")
			}
		})
	}
}

func TestRV64PublicMixedTable64SizeImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-size-import-gates", rv64MixedTable64SizeSource(), artifact)
			defer c.Close()
			cases := []struct {
				name  string
				table *Table
			}{
				{"table32", rv64MixedSizeTestTable(t, false, 1, 4)},
				{"small", rv64MixedSizeTestTable(t, true, 0, 4)},
				{"large-max", rv64MixedSizeTestTable(t, true, 1, 5)},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					defer tc.table.Close()
					in, err := Instantiate(c, NewImports().Table("env", "table", tc.table))
					if in != nil {
						in.Close()
					}
					if err == nil {
						t.Fatal("incompatible mixed table64 import admitted")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64SizeArtifactGates(t *testing.T) {
	mutations := map[string]func(*Compiled){
		"witness":        func(c *Compiled) { c.nativeTable64ABI = nativeTable64SizeABI },
		"feature":        func(c *Compiled) { c.requiredFeatures |= CoreFeatureBulkMemoryOperations },
		"reference-abi":  func(c *Compiled) { c.nativeReferenceCallABI = true },
		"descriptor":     func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"import-table32": func(c *Compiled) { c.TableAddr64 = false },
		"import-limit":   func(c *Compiled) { c.tableImportMax = 0 },
		"local-table32":  func(c *Compiled) { c.extraTables[0].Addr64 = false },
		"local-import":   func(c *Compiled) { c.extraTables[0].ImportKey = "env.other" },
		"local-import-max": func(c *Compiled) {
			c.extraTables[0].ImportHasMax = true
		},
		"signature": func(c *Compiled) { c.Funcs[1].Results[0] = ValI32 },
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
			base := rv64TableStageCompile(t, "mixed-table64-size-gates", rv64MixedTable64SizeSource(), false)
			defer base.Close()
			bad := cloneCompiledMetadata(base.executionView())
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[1].Results = slices.Clone(bad.Funcs[1].Results)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.validateMemo, bad.codeCache = &validateMemo{}, nil
			mutate(bad)
			if rv64MixedTable64Size(bad) || !errors.Is(railshotValidateCompiled(bad), railshot.ErrUnsupported) {
				t.Fatal("hostile mixed table64.size metadata admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "local-import-max" {
				if err != nil || loaded == nil || loaded.extraTables[0].ImportHasMax || !rv64MixedTable64Size(loaded.executionView()) {
					t.Fatal("serialized local table retained stale import maximum", err)
				}
				loaded.Close()
				return
			}
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile mixed table64.size artifact admitted")
			}
		})
	}
}
