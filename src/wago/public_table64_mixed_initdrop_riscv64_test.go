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

func rv64MixedTable64InitDropSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	passiveNull := []byte{0x05, 0x70, 0x01, 0xd0, 0x70, 0x0b}
	body := func(index byte) []byte {
		return []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, index, 0x41, 1, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("init1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("drop", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(passiveNull)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(body(0)),
			wasmtest.Code(body(1)),
			wasmtest.Code([]byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}),
		)),
	)
}

func TestRV64PublicMixedTable64InitDrop(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-initdrop-producer", rv64Table64IndirectInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "mixed-table64-initdrop-consumer", rv64MixedTable64InitDropSource(), artifact)
			defer consumerCode.Close()
			view := consumerCode.executionView()
			if view.nativeTable64ABI != nativeTable64NoABI || !rv64SourceContractValid(view) || view.tableCount() != 2 || view.tableImportCount() != 1 || view.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || !view.NeedsFuncRefDescs {
				t.Fatal("mixed table64.init/drop metadata contract")
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
					t.Fatal("seed mixed imported init row", index, got, err)
				}
			}
			consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			alias, err := consumer.ExportedTable("table0")
			if err != nil || alias != imported {
				t.Fatal("mixed init imported table identity", err)
			}
			local, err := consumer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 3 {
				t.Fatal("mixed init owned table", local, err)
			}
			defer local.Close()

			beforeLocal := slices.Clone(local.desc)
			if got, err := consumer.Invoke("init0", 0, 0xfeedface00000000, 0xdeadbeef00000001); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed imported init dirty i32 carriers", got, err)
			}
			if null, err := imported.EntryIsNull(0); err != nil || !null || !slices.Equal(local.desc, beforeLocal) {
				t.Fatal("mixed imported init routing", null, err)
			}
			beforeImported := slices.Clone(imported.desc)
			if got, err := consumer.Invoke("init1", 2, 0x1234567800000000, 0xffffffff00000001); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed owned init dirty i32 carriers", got, err)
			}
			if !slices.Equal(imported.desc, beforeImported) {
				t.Fatal("mixed owned init changed imported descriptor")
			}

			beforeImported, beforeLocal = slices.Clone(imported.desc), slices.Clone(local.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				dest     uint64
				source   uint64
				count    uint64
			}{
				{"init0", 0, 1 << 32, 0, 0}, {"init0", 0, 2, 0, 1}, {"init0", 0, 0, 1, 1}, {"init0", 0, 0, 0, 2},
				{"init1", 1, ^uint64(0), 0, 0}, {"init1", 1, 3, 0, 1}, {"init1", 1, 0, 1, 1}, {"init1", 1, 0, 0, 2},
			} {
				got, err := consumer.Invoke(tc.name, tc.dest, tc.source, tc.count)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
					t.Fatal("mixed table64.init range did not trap", tc, got, err)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed table64.init trap changed storage", tc)
				}
			}

			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("init0", 1, 0, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed imported init after producer close", got, err)
			}
			if null, err := imported.EntryIsNull(1); err != nil || !null {
				t.Fatal("mixed imported retained init row", null, err)
			}

			beforeImported, beforeLocal = slices.Clone(imported.desc), slices.Clone(local.desc)
			for attempt := 0; attempt < 2; attempt++ {
				if got, err := consumer.Invoke("drop"); err != nil || !slices.Equal(got, []uint64{1}) || !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed elem.drop repeat", attempt, got, err)
				}
			}
			for _, tc := range []struct {
				name string
				dest uint64
			}{{"init0", 2}, {"init1", 3}} {
				if got, err := consumer.Invoke(tc.name, tc.dest, 0, 0); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("mixed dropped init zero range", tc, got, err)
				}
				got, err := consumer.Invoke(tc.name, 0, 0, 1)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed init used dropped segment", tc, got, err)
				}
			}
		})
	}
}

func TestRV64PublicMixedTable64InitDropImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-initdrop-import-gates", rv64MixedTable64InitDropSource(), artifact)
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
						t.Fatal("incompatible mixed table64 init import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64InitDropSourceGates(t *testing.T) {
	base := rv64MixedTable64InitDropSource()
	mutations := map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
		"wrong-table":    bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x0c, 0, 1}, []byte{0xfc, 0x0c, 0, 0}, 1),
		"nonnull":        bytes.Replace(bytes.Clone(base), []byte{0x05, 0x70, 0x01, 0xd0, 0x70, 0x0b}, []byte{0x05, 0x70, 0x01, 0xd2, 0x00, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name == "wrong-table" || name == "nonnull" {
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
