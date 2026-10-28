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

func rv64MixedTable64GetSetSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	body := func(index byte) []byte {
		return []byte{0x20, 0, 0xd0, 0x70, 0x26, index, 0x20, 0, 0x25, index, 0xd1, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("roundtrip0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("roundtrip1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(0)), wasmtest.Code(body(1)))),
	)
}

func TestRV64PublicMixedTable64GetSet(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-getset-producer", rv64Table64IndirectInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "mixed-table64-getset-consumer", rv64MixedTable64GetSetSource(), artifact)
			defer consumerCode.Close()
			view := consumerCode.executionView()
			if view.nativeTable64ABI != nativeTable64NoABI || !rv64SourceContractValid(view) || view.tableCount() != 2 || view.tableImportCount() != 1 {
				t.Fatal("mixed table64.get/set metadata contract")
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
				t.Fatal("mixed get/set imported table identity", err)
			}
			local, err := consumer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 2 {
				t.Fatal("mixed get/set owned table", local, err)
			}
			defer local.Close()
			for index := uint64(0); index < 2; index++ {
				if got, err := producer.Invoke("call", index); err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("seed mixed imported row", index, got, err)
				}
			}
			if got, err := consumer.Invoke("roundtrip0", 0); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed imported null roundtrip", got, err)
			}
			if null, err := imported.EntryIsNull(0); err != nil || !null {
				t.Fatal("mixed imported mutation not visible", null, err)
			}
			if got, err := consumer.Invoke("roundtrip1", 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed owned null roundtrip", got, err)
			}
			before0, before1 := slices.Clone(imported.desc), slices.Clone(local.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				index    uint64
			}{{"roundtrip0", 0, 1 << 32}, {"roundtrip0", 0, ^uint64(0)}, {"roundtrip1", 1, 1 << 32}, {"roundtrip1", 1, ^uint64(0)}} {
				got, err := consumer.Invoke(tc.name, tc.index)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
					t.Fatal("mixed table64 high index did not trap", tc, got, err)
				}
				if !slices.Equal(imported.desc, before0) || !slices.Equal(local.desc, before1) {
					t.Fatal("mixed table64 trap changed storage", tc)
				}
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("roundtrip0", 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed imported get/set after producer close", got, err)
			}
		})
	}
}

func TestRV64PublicMixedTable64GetSetImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-getset-import-gates", rv64MixedTable64GetSetSource(), artifact)
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
						t.Fatal("incompatible mixed table64 get/set import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64GetSetSourceGates(t *testing.T) {
	base := rv64MixedTable64GetSetSource()
	mutations := map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 2, 3}, []byte{0x70, 0x01, 2, 3}, 1),
		"wrong-index":    bytes.Replace(bytes.Clone(base), []byte{0x26, 1}, []byte{0x26, 0}, 1),
		"wrong-body":     bytes.Replace(bytes.Clone(base), []byte{0x25, 1}, []byte{0x25, 0}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name == "wrong-index" || name == "wrong-body" {
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
