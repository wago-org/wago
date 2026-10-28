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

func rv64MixedTable64FillSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	body := func(index byte) []byte {
		return []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, index, 0x41, 1, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fill0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("fill1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(0)), wasmtest.Code(body(1)))),
	)
}

func TestRV64PublicMixedTable64Fill(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "mixed-table64-fill-producer", rv64Table64IndirectInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "mixed-table64-fill-consumer", rv64MixedTable64FillSource(), artifact)
			defer consumerCode.Close()
			view := consumerCode.executionView()
			if view.nativeTable64ABI != nativeTable64NoABI || !rv64SourceContractValid(view) || view.tableCount() != 2 || view.tableImportCount() != 1 || view.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes || !view.NeedsFuncRefDescs {
				t.Fatal("mixed table64.fill metadata contract")
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
					t.Fatal("seed mixed imported fill row", index, got, err)
				}
			}
			consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", imported))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			alias, err := consumer.ExportedTable("table0")
			if err != nil || alias != imported {
				t.Fatal("mixed fill imported table identity", err)
			}
			local, err := consumer.ExportedTable("table1")
			if err != nil || local == imported || local.Size() != 3 {
				t.Fatal("mixed fill owned table", local, err)
			}
			defer local.Close()

			beforeLocal := slices.Clone(local.desc)
			if got, err := consumer.Invoke("fill0", 0, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed imported fill", got, err)
			}
			if null, err := imported.EntryIsNull(0); err != nil || !null {
				t.Fatal("mixed imported fill did not clear row", null, err)
			}
			if null, err := imported.EntryIsNull(1); err != nil || null {
				t.Fatal("mixed imported fill changed adjacent row", null, err)
			}
			if !slices.Equal(local.desc, beforeLocal) {
				t.Fatal("mixed imported fill changed local descriptor")
			}
			beforeImported := slices.Clone(imported.desc)
			if got, err := consumer.Invoke("fill1", 2, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed owned fill", got, err)
			}
			if !slices.Equal(imported.desc, beforeImported) {
				t.Fatal("mixed owned fill changed imported descriptor")
			}
			for _, call := range []struct {
				name  string
				start uint64
			}{{"fill0", 2}, {"fill1", 3}} {
				if got, err := consumer.Invoke(call.name, call.start, 0); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("mixed fill exact-end zero range", call, got, err)
				}
			}

			beforeImported, beforeLocal = slices.Clone(imported.desc), slices.Clone(local.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				start    uint64
				count    uint64
			}{
				{"fill0", 0, 1, 2}, {"fill0", 0, 1 << 32, 0}, {"fill0", 0, 0, 1 << 32}, {"fill0", 0, ^uint64(0), 2},
				{"fill1", 1, 2, 2}, {"fill1", 1, 1 << 32, 0}, {"fill1", 1, 0, 1 << 32}, {"fill1", 1, ^uint64(0), 2},
			} {
				got, err := consumer.Invoke(tc.name, tc.start, tc.count)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
					t.Fatal("mixed table64.fill range did not trap", tc, got, err)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(local.desc, beforeLocal) {
					t.Fatal("mixed table64.fill trap changed storage", tc)
				}
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("fill0", 1, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("mixed imported fill after producer close", got, err)
			}
			if null, err := imported.EntryIsNull(1); err != nil || !null {
				t.Fatal("mixed imported retained row remained nonnull", null, err)
			}
		})
	}
}

func TestRV64PublicMixedTable64FillImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "mixed-table64-fill-import-gates", rv64MixedTable64FillSource(), artifact)
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
						t.Fatal("incompatible mixed table64 fill import admitted or mutated")
					}
				})
			}
		})
	}
}

func TestRV64PublicMixedTable64FillSourceGates(t *testing.T) {
	base := rv64MixedTable64FillSource()
	mutations := map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 1, 4}, []byte{0x70, 0x01, 1, 4}, 1),
		"local-table32":  bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 3, 3}, []byte{0x70, 0x01, 3, 3}, 1),
		"wrong-index":    bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x11, 1}, []byte{0xfc, 0x11, 0}, 1),
		"wrong-body":     bytes.Replace(bytes.Clone(base), []byte{0xfc, 0x11, 0}, []byte{0xfc, 0x0f, 0}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, source)
			if name == "wrong-index" {
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
