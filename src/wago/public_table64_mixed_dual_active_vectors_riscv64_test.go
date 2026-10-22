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

func rv64MixedTable64DualActiveVectorsSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 4, 4)
	importedActive := []byte{0x04, 0x42, 1, 0x0b, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	ownedActive := []byte{0x06, 0x01, 0x42, 1, 0x0b, 0x70, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(importedActive, ownedActive)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 8, 0x0b}),
		)),
	)
}

func TestRV64PublicMixedTable64DualActiveVectors(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-vectors", rv64MixedTable64DualActiveVectorsSource(), artifact)
			view := producerCode.executionView()
			if !rv64ArtifactContractValid(view) {
				t.Fatal("mixed table64 dual-active vector metadata contract")
			}
			readerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-vector-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer readerCode.Close()
			nullerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-vector-nuller", rv64ImportedTable64GetSetInstructionSource(), artifact)
			defer nullerCode.Close()
			imported := rv64MixedSizeTestTable(t, true, 4, 4)
			defer imported.Close()
			producer, err := Instantiate(producerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			owned, err := producer.ExportedTable("table1")
			if err != nil || owned == imported || owned.Size() != 4 {
				t.Fatal("mixed dual-active vector owned table", owned, err)
			}
			defer owned.Close()
			for name, table := range map[string]*Table{"imported": imported, "owned": owned} {
				for index, wantNull := range []bool{true, false, false, true} {
					null, entryErr := table.EntryIsNull(uint64(index))
					if entryErr != nil || null != wantNull {
						t.Fatal(name, "active vector row", index, null, entryErr)
					}
				}
			}
			ownedReader, err := Instantiate(readerCode, NewImports().Table("env", "table", owned))
			if err != nil {
				t.Fatal("attach owned vector reader", err)
			}
			defer ownedReader.Close()
			nuller, err := Instantiate(nullerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			for _, index := range []uint64{1, 2} {
				if got, invokeErr := nuller.Invoke("roundtrip", index); invokeErr != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("clear imported vector row", index, got, invokeErr)
				}
			}
			if err := nuller.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			for index, want := range map[uint64]uint64{1: 7, 2: 8} {
				if got, invokeErr := ownedReader.Invoke("call", index); invokeErr != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("owned vector target after producer retirement", index, got, invokeErr)
				}
			}
			for _, index := range []uint64{0, 3, 1 << 32, ^uint64(0)} {
				got, invokeErr := ownedReader.Invoke("call", index)
				var trap *coreruntime.TrapError
				if invokeErr == nil || got != nil || !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("owned vector null/high row did not trap", index, got, invokeErr)
				}
			}
		})
	}
}

func TestRV64PublicMixedTable64DualActiveVectorsImportedLifetime(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-vector-imported-lifetime", rv64MixedTable64DualActiveVectorsSource(), artifact)
			readerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-vector-imported-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
			defer readerCode.Close()
			imported := rv64MixedSizeTestTable(t, true, 4, 4)
			defer imported.Close()
			producer, err := Instantiate(producerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			owned, err := producer.ExportedTable("table1")
			if err != nil {
				t.Fatal(err)
			}
			if err := owned.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			reader, err := Instantiate(readerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal("attach imported vector reader after producer retirement", err)
			}
			defer reader.Close()
			for index, want := range map[uint64]uint64{1: 7, 2: 8} {
				if got, invokeErr := reader.Invoke("call", index); invokeErr != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("imported vector target after producer retirement", index, got, invokeErr)
				}
			}
		})
	}
}

func TestRV64PublicMixedTable64DualActiveVectorsImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-dual-active-vector-import-gates", rv64MixedTable64DualActiveVectorsSource(), artifact)
			defer c.Close()
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
					in, err := Instantiate(c, NewImports().Table("env", "table", tc.table))
					if in != nil {
						in.Close()
					}
					if err == nil || !slices.Equal(tc.table.desc, before) {
						t.Fatal("incompatible mixed active-vector import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64DualActiveVectorsSourceGates(t *testing.T) {
	base := rv64MixedTable64DualActiveVectorsSource()
	localTable32 := bytes.Clone(base)
	localTable := bytes.LastIndex(localTable32, []byte{0x70, 0x05, 4, 4})
	if localTable < 0 {
		t.Fatal("local table encoding")
	}
	localTable32[localTable+1] = 0x01
	for name, source := range map[string][]byte{
		"first-offset":   bytes.Replace(bytes.Clone(base), []byte{0x04, 0x42, 1, 0x0b}, []byte{0x04, 0x42, 2, 0x0b}, 1),
		"second-table":   bytes.Replace(bytes.Clone(base), []byte{0x06, 0x01, 0x42, 1}, []byte{0x06, 0x00, 0x42, 1}, 1),
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 4, 4}, []byte{0x70, 0x01, 4, 4}, 1),
		"local-table32":  localTable32,
		"target-order":   bytes.Replace(bytes.Clone(base), []byte{0xd2, 0x00, 0x0b, 0xd2, 0x01}, []byte{0xd2, 0x01, 0x0b, 0xd2, 0x00}, 1),
		"second-body":    bytes.Replace(bytes.Clone(base), []byte{0x41, 8, 0x0b}, []byte{0x41, 9, 0x0b}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name != "import-table32" && name != "local-table32" {
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

func TestRV64PublicMixedTable64DualActiveVectorsArtifactGates(t *testing.T) {
	mutations := map[string]func(*Compiled){
		"feature":       func(c *Compiled) { c.requiredFeatures |= CoreFeatureBulkMemoryOperations },
		"reference-abi": func(c *Compiled) { c.nativeReferenceCallABI = true },
		"descriptor":    func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"type-key":      func(c *Compiled) { c.FuncTypeID[1] ^= 1 },
		"import-limit":  func(c *Compiled) { c.tableImportMin = 3 },
		"local-limit":   func(c *Compiled) { c.extraTables[0].Size = 3 },
		"table-export":  func(c *Compiled) { c.tableExports["table1"] = 0 },
		"element-count": func(c *Compiled) { c.Elems = c.Elems[:1] },
		"element-table": func(c *Compiled) { c.Elems[1].TableIndex = 0 },
		"element-value": func(c *Compiled) { c.Elems[1].Values[1].FuncIndex = 0 },
		"entry":         func(c *Compiled) { c.Entry[1]-- },
		"internal-entry": func(c *Compiled) {
			c.InternalEntry[1]--
		},
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			base := rv64TableStageCompile(t, "mixed-table64-dual-active-vector-gates", rv64MixedTable64DualActiveVectorsSource(), false)
			defer base.Close()
			bad := rv64ReferenceImportTailCopy(base)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Funcs = slices.Clone(bad.Funcs)
			for i := range bad.Funcs {
				bad.Funcs[i].Results = slices.Clone(bad.Funcs[i].Results)
			}
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.Elems = slices.Clone(bad.Elems)
			for i := range bad.Elems {
				bad.Elems[i].Offset.Expr = bytes.Clone(bad.Elems[i].Offset.Expr)
				bad.Elems[i].Values = slices.Clone(bad.Elems[i].Values)
			}
			mutate(bad)
			if bad.validate() == nil {
				t.Fatal("hostile mixed active-vector metadata admitted")
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
				t.Fatal("hostile mixed active-vector artifact admitted")
			}
		})
	}
}
