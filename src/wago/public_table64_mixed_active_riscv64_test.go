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

func rv64MixedTable64ActiveElementSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 3, 3)
	active := []byte{0x04, 0x42, 1, 0x0b, 0x01, 0xd2, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(active)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))),
	)
}

func TestRV64PublicMixedTable64ActiveElement(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-active-producer", rv64MixedTable64ActiveElementSource(), artifact)
			readerCode := rv64TableStageCompile(t, "mixed-table64-active-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer readerCode.Close()
			if !rv64ArtifactContractValid(producerCode.executionView()) {
				t.Fatal("mixed table64 active metadata contract")
			}
			imported := rv64MixedSizeTestTable(t, true, 3, 3)
			defer imported.Close()
			before := slices.Clone(imported.desc)
			producer, err := Instantiate(producerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			alias, err := producer.ExportedTable("table0")
			if err != nil || alias != imported {
				t.Fatal("mixed active imported identity", err)
			}
			local, err := producer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 1 {
				t.Fatal("mixed active local table", local, err)
			}
			defer local.Close()
			if slices.Equal(imported.desc, before) {
				t.Fatal("mixed active initializer did not publish imported row")
			}
			for index, wantNull := range []bool{true, false, true} {
				null, err := imported.EntryIsNull(uint64(index))
				if err != nil || null != wantNull {
					t.Fatal("mixed active imported row", index, null, err)
				}
			}
			if null, err := local.EntryIsNull(0); err != nil || !null {
				t.Fatal("mixed active changed local descriptor", null, err)
			}
			reader, err := Instantiate(readerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			call := func(reader *Instance) {
				t.Helper()
				if got, err := reader.Invoke("call", 1); err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("mixed active retained call", got, err)
				}
				for _, index := range []uint64{0, 2, 1 << 32, ^uint64(0)} {
					got, err := reader.Invoke("call", index)
					var trap *coreruntime.TrapError
					if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
						t.Fatal("mixed active null/high row did not trap", index, got, err)
					}
				}
			}
			call(reader)
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			retainedReader, err := Instantiate(readerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal("attach reader after producer retirement", err)
			}
			defer retainedReader.Close()
			call(retainedReader)
		})
	}
}

func TestRV64PublicMixedTable64ActiveElementImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-active-import-gates", rv64MixedTable64ActiveElementSource(), artifact)
			defer c.Close()
			for _, tc := range []struct {
				name  string
				table *Table
			}{
				{"table32", rv64MixedSizeTestTable(t, false, 3, 3)},
				{"small", rv64MixedSizeTestTable(t, true, 2, 3)},
				{"large-max", rv64MixedSizeTestTable(t, true, 3, 4)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					defer tc.table.Close()
					before := slices.Clone(tc.table.desc)
					in, err := Instantiate(c, NewImports().Table("env", "table", tc.table))
					if in != nil {
						in.Close()
					}
					if err == nil || !slices.Equal(tc.table.desc, before) {
						t.Fatal("incompatible mixed active import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64ActiveElementSourceGates(t *testing.T) {
	base := rv64MixedTable64ActiveElementSource()
	for name, source := range map[string][]byte{
		"offset":         bytes.Replace(bytes.Clone(base), []byte{0x42, 1, 0x0b}, []byte{0x42, 2, 0x0b}, 1),
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 1}, []byte{0x70, 0x01, 1, 1}, 1),
		"target":         bytes.Replace(bytes.Clone(base), []byte{0x41, 7, 0x0b}, []byte{0x41, 8, 0x0b}, 1),
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
}

func TestRV64PublicMixedTable64ActiveElementArtifactGates(t *testing.T) {
	mutations := map[string]func(*Compiled){
		"witness":        func(c *Compiled) { c.nativeTable64ABI = nativeTable64SizeABI },
		"feature":        func(c *Compiled) { c.requiredFeatures |= CoreFeatureBulkMemoryOperations },
		"missing-ref":    func(c *Compiled) { c.requiredFeatures &^= CoreFeatureReferenceTypes },
		"reference-abi":  func(c *Compiled) { c.nativeReferenceCallABI = true },
		"descriptor":     func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"type-key":       func(c *Compiled) { c.FuncTypeID[0] ^= 1 },
		"import-table32": func(c *Compiled) { c.TableAddr64 = false },
		"import-limit":   func(c *Compiled) { c.tableImportMin = 2 },
		"local-table32":  func(c *Compiled) { c.extraTables[0].Addr64 = false },
		"local-import":   func(c *Compiled) { c.extraTables[0].ImportKey = "env.other" },
		"local-import-max": func(c *Compiled) {
			c.extraTables[0].ImportHasMax = true
		},
		"signature":      func(c *Compiled) { c.Funcs[0].Results[0] = ValI64 },
		"table-export":   func(c *Compiled) { c.tableExports["table1"] = 0 },
		"element-table":  func(c *Compiled) { c.Elems[0].TableIndex = 1 },
		"element-offset": func(c *Compiled) { c.Elems[0].Offset.Expr = []byte{0x42, 2, 0x0b} },
		"element-value":  func(c *Compiled) { c.Elems[0].Values[0].FuncIndex = 1 },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[0] ^= 1
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			base := rv64TableStageCompile(t, "mixed-table64-active-gates", rv64MixedTable64ActiveElementSource(), false)
			defer base.Close()
			bad := rv64ReferenceImportTailCopy(base)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.Elems = slices.Clone(bad.Elems)
			bad.Elems[0].Offset.Expr = bytes.Clone(bad.Elems[0].Offset.Expr)
			bad.Elems[0].Values = slices.Clone(bad.Elems[0].Values)
			mutate(bad)
			if bad.validate() == nil {
				t.Fatal("hostile mixed active metadata admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "local-import-max" {
				if err != nil || loaded == nil || loaded.extraTables[0].ImportHasMax || !rv64ArtifactContractValid(loaded.executionView()) {
					t.Fatal("serialized local table retained stale import maximum", err)
				}
				loaded.Close()
				return
			}
			if name == "missing-ref" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || !rv64ArtifactContractValid(loaded.executionView()) {
					t.Fatal("serialized mixed active structural feature was not restored", err)
				}
				loaded.Close()
				return
			}
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile mixed active artifact admitted")
			}
		})
	}
}
