//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MixedTable64DualActiveElementsSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 3, 3)
	importedActive := []byte{0x04, 0x42, 1, 0x0b, 0x01, 0xd2, 0x00, 0x0b}
	ownedActive := []byte{0x06, 0x01, 0x42, 1, 0x0b, 0x70, 0x01, 0xd2, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(importedActive, ownedActive)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))),
	)
}

func TestRV64PublicMixedTable64DualActiveElements(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			checkRows := func(name string, table *Table) {
				t.Helper()
				for index, wantNull := range []bool{true, false, true} {
					null, err := table.EntryIsNull(uint64(index))
					if err != nil || null != wantNull {
						t.Fatal(name, "active row", index, null, err)
					}
				}
			}
			call := func(name string, reader *Instance) {
				t.Helper()
				if got, err := reader.Invoke("call", 1); err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal(name, "retained call", got, err)
				}
				for _, index := range []uint64{0, 2, 1 << 32, ^uint64(0)} {
					got, err := reader.Invoke("call", index)
					var trap *coreruntime.TrapError
					if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
						t.Fatal(name, "null/high row did not trap", index, got, err)
					}
				}
			}
			t.Run("imported-root", func(t *testing.T) {
				producerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-imported-producer", rv64MixedTable64DualActiveElementsSource(), artifact)
				readerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-imported-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
				defer readerCode.Close()
				if !rv64SourceContractValid(producerCode.executionView()) {
					t.Fatal("mixed table64 dual-active metadata contract")
				}
				imported := rv64MixedSizeTestTable(t, true, 3, 3)
				defer imported.Close()
				producer, err := Instantiate(producerCode, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				owned, err := producer.ExportedTable("table1")
				if err != nil || owned == imported || owned.Size() != 3 {
					t.Fatal("mixed dual-active owned table", owned, err)
				}
				checkRows("imported", imported)
				checkRows("owned", owned)
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
					t.Fatal("attach imported reader after producer retirement", err)
				}
				defer reader.Close()
				call("imported", reader)
			})
			t.Run("owned-lease", func(t *testing.T) {
				producerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-owned-producer", rv64MixedTable64DualActiveElementsSource(), artifact)
				readerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-owned-reader", rv64ImportedTable64IndirectInstructionSource(), artifact)
				nullerCode := rv64TableStageCompile(t, "mixed-table64-dual-active-imported-nuller", rv64ImportedTable64GetSetInstructionSource(), artifact)
				defer readerCode.Close()
				defer nullerCode.Close()
				imported := rv64MixedSizeTestTable(t, true, 3, 3)
				defer imported.Close()
				producer, err := Instantiate(producerCode, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				owned, err := producer.ExportedTable("table1")
				if err != nil {
					t.Fatal(err)
				}
				defer owned.Close()
				reader, err := Instantiate(readerCode, NewImports().Table("env", "table", owned))
				if err != nil {
					t.Fatal("attach owned reader before producer retirement", err)
				}
				defer reader.Close()
				nuller, err := Instantiate(nullerCode, NewImports().Table("env", "table", imported))
				if err != nil {
					t.Fatal(err)
				}
				if got, err := nuller.Invoke("roundtrip", 1); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("clear unrelated imported root", got, err)
				}
				if err := nuller.Close(); err != nil {
					t.Fatal(err)
				}
				if null, err := imported.EntryIsNull(1); err != nil || !null {
					t.Fatal("unrelated imported root remained live", null, err)
				}
				if err := producerCode.Close(); err != nil {
					t.Fatal(err)
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				call("owned", reader)
			})
		})
	}
}

func TestRV64PublicMixedTable64DualActiveElementsImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-dual-active-import-gates", rv64MixedTable64DualActiveElementsSource(), artifact)
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
						t.Fatal("incompatible mixed dual-active import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64DualActiveElementsSourceGates(t *testing.T) {
	base := rv64MixedTable64DualActiveElementsSource()
	localTable32 := bytes.Clone(base)
	localTable := bytes.LastIndex(localTable32, []byte{0x70, 0x05, 3, 3})
	if localTable < 0 {
		t.Fatal("local table encoding")
	}
	localTable32[localTable+1] = 0x01
	for name, source := range map[string][]byte{
		"first-offset":   bytes.Replace(bytes.Clone(base), []byte{0x04, 0x42, 1, 0x0b}, []byte{0x04, 0x42, 2, 0x0b}, 1),
		"second-table":   bytes.Replace(bytes.Clone(base), []byte{0x06, 0x01, 0x42, 1}, []byte{0x06, 0x00, 0x42, 1}, 1),
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
		"local-table32":  localTable32,
		"target":         bytes.Replace(bytes.Clone(base), []byte{0x41, 7, 0x0b}, []byte{0x41, 8, 0x0b}, 1),
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
