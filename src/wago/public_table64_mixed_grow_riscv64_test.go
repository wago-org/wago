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

func rv64MixedTable64GrowSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	body := func(index byte) []byte {
		return []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, index, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("grow1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(0)), wasmtest.Code(body(1)))),
	)
}

func TestRV64PublicMixedTable64Grow(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-grow-producer", rv64Table64SizeInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "mixed-table64-grow-consumer", rv64MixedTable64GrowSource(), artifact)
			defer consumerCode.Close()
			view := consumerCode.executionView()
			if view.nativeTable64ABI != nativeTable64ImportedGrowABI || !rv64MixedTable64Grow(view) || view.tableCount() != 2 || view.tableImportCount() != 1 || view.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes || !view.NeedsFuncRefDescs {
				t.Fatal("mixed table64.grow metadata contract")
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
			consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			alias, err := consumer.ExportedTable("table0")
			if err != nil || alias != imported {
				t.Fatal("mixed grow imported table identity", err)
			}
			local, err := consumer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 2 {
				t.Fatal("mixed grow owned table", local, err)
			}
			defer local.Close()

			beforeImported, beforeLocal := slices.Clone(imported.desc), slices.Clone(local.desc)
			for _, delta := range []uint64{1 << 32, ^uint64(0)} {
				if got, err := consumer.Invoke("grow0", delta); err != nil || !slices.Equal(got, []uint64{^uint64(0)}) || imported.Size() != 2 || local.Size() != 2 {
					t.Fatal("mixed imported high grow changed size", delta, got, imported.Size(), local.Size(), err)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed imported high grow changed storage", delta)
				}
				if got, err := consumer.Invoke("grow1", delta); err != nil || !slices.Equal(got, []uint64{^uint64(0)}) || imported.Size() != 2 || local.Size() != 2 {
					t.Fatal("mixed owned high grow changed size", delta, got, imported.Size(), local.Size(), err)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed owned high grow changed storage", delta)
				}
			}
			if got, err := consumer.Invoke("grow0", 1); err != nil || !slices.Equal(got, []uint64{2}) || imported.Size() != 3 || local.Size() != 2 {
				t.Fatal("mixed imported grow selected wrong descriptor", got, imported.Size(), local.Size(), err)
			}
			if got, err := consumer.Invoke("grow1", 1); err != nil || !slices.Equal(got, []uint64{2}) || imported.Size() != 3 || local.Size() != 3 {
				t.Fatal("mixed owned grow selected wrong descriptor", got, imported.Size(), local.Size(), err)
			}
			for _, table := range []*Table{imported, local} {
				if null, err := table.EntryIsNull(2); err != nil || !null {
					t.Fatal("mixed table64.grow manufactured nonnull row", table, null, err)
				}
			}
			beforeImported, beforeLocal = slices.Clone(imported.desc), slices.Clone(local.desc)
			if got, err := consumer.Invoke("grow1", 1); err != nil || !slices.Equal(got, []uint64{^uint64(0)}) || local.Size() != 3 || imported.Size() != 3 {
				t.Fatal("mixed owned max failure changed descriptors", got, imported.Size(), local.Size(), err)
			}
			if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
				t.Fatal("mixed owned max failure changed storage")
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("grow0", 1); err != nil || !slices.Equal(got, []uint64{3}) || imported.Size() != 4 || local.Size() != 3 {
				t.Fatal("mixed imported grow after producer close", got, imported.Size(), local.Size(), err)
			}
			if null, err := imported.EntryIsNull(3); err != nil || !null {
				t.Fatal("mixed imported retained grow row", null, err)
			}
		})
	}
}

func TestRV64PublicMixedTable64GrowImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-grow-import-gates", rv64MixedTable64GrowSource(), artifact)
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
					before := slices.Clone(tc.table.desc)
					in, err := Instantiate(c, NewImports().Table("env", "table", tc.table))
					if in != nil {
						in.Close()
					}
					if err == nil || !slices.Equal(tc.table.desc, before) {
						t.Fatal("incompatible mixed table64 grow import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64GrowSourceGates(t *testing.T) {
	base := rv64MixedTable64GrowSource()
	mutations := map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 2, 3}, []byte{0x70, 0x01, 2, 3}, 1),
		"wrong-index":    bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0f, 1}, []byte{0xfc, 0x0f, 0}, 1),
		"wrong-body":     bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0f, 0}, []byte{0xfc, 0x10, 0}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("nearby mixed table64.grow source admitted")
			}
		})
	}
}

func TestRV64PublicMixedTable64GrowArtifactGates(t *testing.T) {
	mutations := map[string]func(*Compiled){
		"witness":        func(c *Compiled) { c.nativeTable64ABI = nativeTable64GrowABI },
		"feature":        func(c *Compiled) { c.requiredFeatures |= CoreFeatureBulkMemoryOperations },
		"reference-abi":  func(c *Compiled) { c.nativeReferenceCallABI = true },
		"descriptor":     func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"import-table32": func(c *Compiled) { c.TableAddr64 = false },
		"import-limit":   func(c *Compiled) { c.tableImportMax = 0 },
		"local-table32":  func(c *Compiled) { c.extraTables[0].Addr64 = false },
		"local-import":   func(c *Compiled) { c.extraTables[0].ImportKey = "env.other" },
		"signature":      func(c *Compiled) { c.Funcs[1].Results[0] = ValI32 },
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
			base := rv64TableStageCompile(t, "mixed-table64-grow-gates", rv64MixedTable64GrowSource(), false)
			defer base.Close()
			bad := cloneCompiledMetadata(base.executionView())
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[1].Results = slices.Clone(bad.Funcs[1].Results)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.validateMemo, bad.codeCache = &validateMemo{}, nil
			mutate(bad)
			if rv64MixedTable64Grow(bad) || !errors.Is(railshotValidateCompiled(bad), railshot.ErrUnsupported) {
				t.Fatal("hostile mixed table64.grow metadata admitted")
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
				t.Fatal("hostile mixed table64.grow artifact admitted")
			}
		})
	}
}
