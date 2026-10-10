//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64StateOnlyTable64Source(min, max byte, exports ...string) []byte {
	sections := [][]byte{wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, min, max}))}
	if len(exports) != 0 {
		entries := make([][]byte, len(exports))
		for i, name := range exports {
			entries[i] = wasmtest.ExportEntry(name, byte(wasm.ExternTable), 0)
		}
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(entries...)))
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicStateOnlyTable64Export(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-table64", rv64StateOnlyTable64Source(2, 4, "table", "alias"), artifact)
			if !c.TableAddr64 || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !compiledStructuralRequiredFeatures(c).IsEnabled(CoreFeatureTable64) || !rv64StateOnlyTable64(c) {
				t.Fatal("table64 metadata contract")
			}
			rv64CheckStateOnlyCode(t, c, 0)
			instances := make([]*Instance, 0, 2)
			tables := make([]*Table, 0, 2)
			for range 2 {
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = in.Close() })
				instances = append(instances, in)
				table, err := in.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = table.Close() })
				tables = append(tables, table)
				alias, err := in.ExportedTable("alias")
				if err != nil || alias != table {
					t.Fatal("table64 export alias identity", err)
				}
				if table.Size() != 2 {
					t.Fatal("table64 size", table.Size())
				}
				for i := uint64(0); i < 2; i++ {
					if null, err := table.EntryIsNull(i); err != nil || !null {
						t.Fatal("table64 null entry", i, null, err)
					}
				}
				if _, err := table.EntryIsNull(2); err == nil {
					t.Fatal("table64 public index bound ignored")
				}
			}
			if tables[0] == tables[1] {
				t.Fatal("owned table64 storage shared between instances")
			}
			rv64CheckStateOnlyCode(t, c, 2)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			for i, table := range tables {
				if null, err := table.EntryIsNull(0); err != nil || !null {
					t.Fatal("table64 did not outlive compiled handle", i, null, err)
				}
				if err := table.Close(); err != nil {
					t.Fatal(err)
				}
				if err := instances[i].Close(); err != nil {
					t.Fatal(err)
				}
			}
			rv64CheckStateOnlyCode(t, c, 0)
		})
	}
}

func TestRV64PublicStateOnlyTable64CompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		t.Skip("actual precompiled runtime only")
	}
	if c, err := Compile(nil, rv64StateOnlyTable64Source(0, 4, "table")); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("compiler-free table64 witness used source compilation", err)
	}
}

func rv64Table64SizeInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x10, 0, 0x0b}))),
	)
}

func rv64ImportedTable64SizeInstructionSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x10, 0, 0x0b}))),
	)
}

func rv64ImportedTable64GetSetInstructionSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x20, 0, 0x25, 0, 0xd1, 0x0b}))),
	)
}

func rv64Table64UnsupportedInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x10, 0, 0x1a, 0x42, 0, 0x0b}))),
	)
}

func rv64Table64GrowInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}))),
	)
}

func rv64Table64GrowNoMaxSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x04, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}))),
	)
}

func rv64Table64GetSetInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x20, 0, 0x25, 0, 0xd1, 0x0b}))),
	)
}

func rv64Table64IndirectInstructionSource() []byte {
	declaredTarget := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 1), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(9, wasmtest.Vec(declaredTarget)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x26, 0, 0x20, 0, 0x11, 0, 0, 0x0b}),
		)),
	)
}

func rv64Table64FillInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fill", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, 0, 0x41, 1, 0x0b}))),
	)
}

func rv64Table64CopyInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 5, 5})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 0, 0, 0x41, 1, 0x0b}))),
	)
}

func rv64Table64InitDropInstructionSource() []byte {
	passiveNull := []byte{0x05, 0x70, 0x01, 0xd0, 0x70, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("drop", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
		)),
		wasmtest.Section(9, wasmtest.Vec(passiveNull)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, 0, 0x41, 1, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}),
		)),
	)
}

func rv64RelabelledTable32FunctionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("value", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x42, 7, 0x0b}))),
	)
}

func TestRV64PublicOwnedTable64Size(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-size", rv64Table64SizeInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64SizeABI || !rv64OwnedTable64Size(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) {
				t.Fatal("table64.size metadata contract")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 2 {
				t.Fatal("table64.size export", table, err)
			}
			t.Cleanup(func() { _ = table.Close() })
			for range 3 {
				if got, err := in.Invoke("size"); err != nil || !slices.Equal(got, []uint64{2}) {
					t.Fatal("table64.size", got, err)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("size"); err != nil || !slices.Equal(got, []uint64{2}) {
				t.Fatal("table64.size after compiled close", got, err)
			}
		})
	}
}

func TestRV64PublicImportedTable64Size(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "imported-table64-size-producer", rv64Table64GrowInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "imported-table64-size-consumer", rv64ImportedTable64SizeInstructionSource(), artifact)
			view := consumerCode.executionView()
			importDef, imported := view.tableImportAt(0)
			if view.nativeTable64ABI != nativeTable64ImportedSizeABI || !rv64ImportedTable64Size(view) || !view.requiredFeatures.IsEnabled(CoreFeatureTable64) || !imported || !importDef.Addr64 {
				t.Fatalf("imported table64.size metadata contract: witness=%d qualified=%v addr64=%v imported=%v def=%+v", view.nativeTable64ABI, rv64ImportedTable64Size(view), view.TableAddr64, imported, importDef)
			}
			producer, err := Instantiate(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = producer.Close() })
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = consumer.Close() })
			alias, err := consumer.ExportedTable("table")
			if err != nil || alias != table {
				t.Fatal("imported table64 export identity", err)
			}
			if got, err := consumer.Invoke("size"); err != nil || !slices.Equal(got, []uint64{2}) {
				t.Fatal("initial imported table64.size", got, err)
			}
			if got, err := producer.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{2}) {
				t.Fatal("producer table64.grow", got, err)
			}
			for range 3 {
				if got, err := consumer.Invoke("size"); err != nil || !slices.Equal(got, []uint64{3}) {
					t.Fatal("shared imported table64.size", got, err)
				}
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("size"); err != nil || !slices.Equal(got, []uint64{3}) {
				t.Fatal("imported table64.size after producer close", got, err)
			}
		})
	}
}

func TestRV64PublicImportedTable64SizeImportGates(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "imported-table64-size-consumer", rv64ImportedTable64SizeInstructionSource(), artifact)
			newTable := func(addr64 bool, min, max uint32) *Table {
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
				t.Cleanup(func() { _ = table.Close() })
				return table
			}
			for name, table := range map[string]*Table{
				"table32":    newTable(false, 1, 4),
				"minimum":    newTable(true, 0, 4),
				"maximum":    newTable(true, 1, 5),
				"undersized": newTable(true, 0, 1),
			} {
				t.Run(name, func(t *testing.T) {
					if in, err := Instantiate(c, NewImports().Table("env", "table", table)); err == nil {
						_ = in.Close()
						t.Fatal("incompatible imported table64 admitted")
					}
				})
			}
			table := newTable(true, 1, 4)
			in, err := Instantiate(c, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			if got, err := in.Invoke("size"); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("host imported table64.size", got, err)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicImportedTable64GetSet(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producerCode := rv64TableStageCompile(t, "imported-table64-getset-producer", rv64Table64IndirectInstructionSource(), artifact)
			consumerCode := rv64TableStageCompile(t, "imported-table64-getset-consumer", rv64ImportedTable64GetSetInstructionSource(), artifact)
			view := consumerCode.executionView()
			if view.nativeTable64ABI != nativeTable64ImportedGetSetABI || !rv64ImportedTable64GetSet(view) || !view.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) {
				t.Fatal("imported table64.get/set metadata contract")
			}
			producer, err := Instantiate(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = producer.Close() })
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = table.Close() })
			consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = consumer.Close() })
			if got, err := producer.Invoke("call", 0); err != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("seed imported table64 row", got, err)
			}
			if null, err := table.EntryIsNull(0); err != nil || null {
				t.Fatal("producer did not publish canonical row", null, err)
			}
			if got, err := consumer.Invoke("roundtrip", 0); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("imported table64 null roundtrip", got, err)
			}
			if null, err := table.EntryIsNull(0); err != nil || !null {
				t.Fatal("imported table64 mutation not visible", null, err)
			}
			if got, err := producer.Invoke("call", 0); err != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("reseed imported table64 row", got, err)
			}
			before := slices.Clone(table.desc)
			for _, index := range []uint64{1 << 32, ^uint64(0)} {
				got, err := consumer.Invoke("roundtrip", index)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
					t.Fatal("imported table64 high index did not trap", index, got, err)
				}
				if !slices.Equal(table.desc, before) {
					t.Fatal("imported table64 trap changed storage", index)
				}
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("roundtrip", 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("imported table64 get/set after producer close", got, err)
			}
		})
	}
}

func TestRV64PublicOwnedTable64Grow(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-grow", rv64Table64GrowInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64GrowABI || !rv64OwnedTable64Grow(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) {
				t.Fatal("table64.grow metadata contract")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = table.Close() })
			for _, step := range []struct {
				delta, old uint64
				size       int
			}{
				{^uint64(0), ^uint64(0), 2},
				{1, 2, 3},
				{1, 3, 4},
				{1, ^uint64(0), 4},
				{1 << 32, ^uint64(0), 4},
			} {
				got, err := in.Invoke("grow", step.delta)
				if err != nil || !slices.Equal(got, []uint64{step.old}) || table.Size() != step.size {
					t.Fatal("table64.grow", step, got, table.Size(), err)
				}
			}
		})
	}
}

func TestRV64PublicOwnedTable64GetSet(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-get-set", rv64Table64GetSetInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64GetSetABI || !rv64OwnedTable64GetSet(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) {
				t.Fatal("table64 get/set metadata contract")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 2 {
				t.Fatal("table64 get/set export", table, err)
			}
			t.Cleanup(func() { _ = table.Close() })
			for _, index := range []uint64{0, 1} {
				if got, err := in.Invoke("roundtrip", index); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("table64 get/set", index, got, err)
				}
			}
			before := slices.Clone(table.desc)
			for _, index := range []uint64{2, 1 << 32, ^uint64(0)} {
				got, err := in.Invoke("roundtrip", index)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
					t.Fatal("table64 get/set index did not trap", index, got, err)
				}
				if !slices.Equal(table.desc, before) || table.Size() != 2 {
					t.Fatal("table64 get/set trap changed storage", index)
				}
			}
			if got, err := in.Invoke("roundtrip", 0); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("table64 get/set reuse after trap", got, err)
			}
		})
	}
}

func TestRV64PublicOwnedTable64Indirect(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-indirect", rv64Table64IndirectInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64IndirectABI || !rv64OwnedTable64Indirect(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
				t.Fatalf("table64 indirect-call metadata contract: witness=%d qualified=%v features=%#x funcs=%+v refs=%v descs=%v typeids=%v elems=%+v passive=%+v", c.nativeTable64ABI, rv64OwnedTable64Indirect(c), c.requiredFeatures, c.Funcs, c.nativeReferenceFunctionABI, c.NeedsFuncRefDescs, c.FuncTypeID, c.Elems, c.passiveElems)
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 2 {
				t.Fatal("table64 indirect-call export", table, err)
			}
			t.Cleanup(func() { _ = table.Close() })
			for _, index := range []uint64{0, 1} {
				if got, err := in.Invoke("call", index); err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("table64 indirect call", index, got, err)
				}
			}
			before := slices.Clone(table.desc)
			for _, index := range []uint64{2, 1 << 32, ^uint64(0)} {
				got, err := in.Invoke("call", index)
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
					t.Fatal("table64 indirect index did not trap", index, got, err)
				}
				if !slices.Equal(table.desc, before) || table.Size() != 2 {
					t.Fatal("table64 indirect trap changed storage", index)
				}
			}
			if got, err := in.Invoke("call", 0); err != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("table64 indirect reuse after trap", got, err)
			}
		})
	}
}

func TestRV64PublicOwnedTable64Fill(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-fill", rv64Table64FillInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64FillABI || !rv64OwnedTable64Fill(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) || c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
				t.Fatalf("table64.fill metadata contract: witness=%d qualified=%v features=%#x funcs=%+v refs=%v descs=%v", c.nativeTable64ABI, rv64OwnedTable64Fill(c), c.requiredFeatures, c.Funcs, c.nativeReferenceFunctionABI, c.NeedsFuncRefDescs)
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 4 {
				t.Fatal("table64.fill export", table, err)
			}
			t.Cleanup(func() { _ = table.Close() })
			if len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
				t.Fatal("table64.fill missing canonical local descriptor")
			}
			row := in.funcRefDescs[coreruntime.FuncRefDescBytes : coreruntime.FuncRefDescBytes+coreruntime.TableEntryBytes]
			for i := uint64(0); i < 4; i++ {
				copy(table.desc[8+int(i)*coreruntime.TableEntryBytes:], row)
			}
			expected := slices.Clone(table.desc)
			clear(expected[8+coreruntime.TableEntryBytes : 8+3*coreruntime.TableEntryBytes])
			if got, err := in.Invoke("fill", 1, 2); err != nil || !slices.Equal(got, []uint64{1}) || !slices.Equal(table.desc, expected) {
				t.Fatal("table64.fill did not clear exact canonical rows", got, err)
			}
			if got, err := in.Invoke("fill", 4, 0); err != nil || !slices.Equal(got, []uint64{1}) || !slices.Equal(table.desc, expected) {
				t.Fatal("table64.fill exact-end zero range", got, err)
			}
			for i, wantNull := range []bool{false, true, true, false} {
				if null, err := table.EntryIsNull(uint64(i)); err != nil || null != wantNull {
					t.Fatal("table64.fill entry state", i, null, wantNull, err)
				}
			}
			before := slices.Clone(table.desc)
			for _, invalid := range [][2]uint64{{3, 2}, {1 << 32, 0}, {0, 1 << 32}, {^uint64(0), 2}} {
				got, err := in.Invoke("fill", invalid[0], invalid[1])
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
					t.Fatal("table64.fill range did not trap", invalid, got, err)
				}
				if !slices.Equal(table.desc, before) || table.Size() != 4 {
					t.Fatal("table64.fill trap changed storage", invalid)
				}
			}
			if got, err := in.Invoke("fill", 0, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("table64.fill reuse after trap", got, err)
			}
			if null, err := table.EntryIsNull(0); err != nil || !null {
				t.Fatal("table64.fill reuse did not clear row zero", null, err)
			}
			if got, err := in.Invoke("fill", 3, 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("table64.fill final cleanup", got, err)
			}
			for i := uint64(0); i < 4; i++ {
				if null, err := table.EntryIsNull(i); err != nil || !null {
					t.Fatal("table64.fill final null entry", i, null, err)
				}
			}
		})
	}
}

func TestRV64PublicOwnedTable64FillArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-fill-admission", rv64Table64FillInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"indirect-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64IndirectABI
		},
		"first-param":  func(v *Compiled) { v.Funcs[0].Params[0] = ValI32 },
		"second-param": func(v *Compiled) { v.Funcs[0].Params[1] = ValI32 },
		"result":       func(v *Compiled) { v.Funcs[0].Results[0] = ValI64 },
		"missing-ref":  func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"no-max":       func(v *Compiled) { v.TableHasMax = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 12)
		},
		"extra-function": func(v *Compiled) { v.Funcs = append(v.Funcs, FuncSig{}) },
		"element":        func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32":        func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64.fill metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid table64.fill artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTable64Copy(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-copy", rv64Table64CopyInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64CopyABI || !rv64OwnedTable64Copy(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
				t.Fatalf("table64.copy metadata contract: witness=%d qualified=%v features=%#x funcs=%+v", c.nativeTable64ABI, rv64OwnedTable64Copy(c), c.requiredFeatures, c.Funcs)
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 5 || len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
				t.Fatal("table64.copy export/descriptor", table, err)
			}
			t.Cleanup(func() { _ = table.Close() })
			row := in.funcRefDescs[coreruntime.FuncRefDescBytes : coreruntime.FuncRefDescBytes+coreruntime.TableEntryBytes]
			seed := func() []byte {
				clear(table.desc[8:])
				for _, i := range []int{0, 2, 4} {
					copy(table.desc[8+i*coreruntime.TableEntryBytes:], row)
				}
				return slices.Clone(table.desc)
			}
			invoke := func(dst, src, count uint64) {
				t.Helper()
				if got, err := in.Invoke("copy", dst, src, count); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("table64.copy valid range", dst, src, count, got, err)
				}
			}
			original := seed()
			want := slices.Clone(original)
			copy(want[8+coreruntime.TableEntryBytes:], want[8:8+4*coreruntime.TableEntryBytes])
			invoke(1, 0, 4)
			if !slices.Equal(table.desc, want) {
				t.Fatal("table64.copy backwards overlap")
			}
			original = seed()
			want = slices.Clone(original)
			copy(want[8:8+4*coreruntime.TableEntryBytes], want[8+coreruntime.TableEntryBytes:])
			invoke(0, 1, 4)
			if !slices.Equal(table.desc, want) {
				t.Fatal("table64.copy forwards overlap")
			}
			invoke(5, 5, 0)
			before := slices.Clone(table.desc)
			for _, invalid := range [][3]uint64{{4, 0, 2}, {0, 4, 2}, {1 << 32, 0, 0}, {0, 1 << 32, 0}, {0, 0, 1 << 32}, {^uint64(0), 0, 2}, {0, ^uint64(0), 2}} {
				got, err := in.Invoke("copy", invalid[0], invalid[1], invalid[2])
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
					t.Fatal("table64.copy range did not trap", invalid, got, err)
				}
				if !slices.Equal(table.desc, before) || table.Size() != 5 {
					t.Fatal("table64.copy trap changed storage", invalid)
				}
			}
			invoke(0, 1, 1)
			if slices.Equal(table.desc, before) {
				t.Fatal("table64.copy reuse after trap did not move row")
			}
			clear(table.desc[8:])
		})
	}
}

func TestRV64PublicOwnedTable64CopyArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-copy-admission", rv64Table64CopyInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"fill-witness":    func(v *Compiled) { v.nativeTable64ABI = nativeTable64FillABI },
		"first-param":     func(v *Compiled) { v.Funcs[0].Params[0] = ValI32 },
		"second-param":    func(v *Compiled) { v.Funcs[0].Params[1] = ValI32 },
		"third-param":     func(v *Compiled) { v.Funcs[0].Params[2] = ValI32 },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI64 },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"missing-bulk":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"no-max":          func(v *Compiled) { v.TableHasMax = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 11)
		},
		"extra-function": func(v *Compiled) { v.Funcs = append(v.Funcs, FuncSig{}) },
		"element":        func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32":        func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64.copy metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid table64.copy artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTable64InitDrop(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-init-drop", rv64Table64InitDropInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64InitDropABI || !rv64OwnedTable64InitDrop(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
				t.Fatalf("table64.init/drop metadata contract: witness=%d qualified=%v features=%#x funcs=%+v elems=%+v passive=%+v values=%+v", c.nativeTable64ABI, rv64OwnedTable64InitDrop(c), c.requiredFeatures, c.Funcs, c.Elems, c.passiveElems, c.ValueTypes)
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 4 || len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
				t.Fatal("table64.init/drop export/descriptor", table, err)
			}
			t.Cleanup(func() { _ = table.Close() })
			row := in.funcRefDescs[coreruntime.FuncRefDescBytes : coreruntime.FuncRefDescBytes+coreruntime.TableEntryBytes]
			for i := 0; i < 4; i++ {
				copy(table.desc[8+i*coreruntime.TableEntryBytes:], row)
			}
			invokeInit := func(dst, src, count uint64) {
				t.Helper()
				if got, err := in.Invoke("init", dst, src, count); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("table64.init valid range", dst, src, count, got, err)
				}
			}
			invokeInit(1, 0, 1)
			if null, err := table.EntryIsNull(1); err != nil || !null {
				t.Fatal("table64.init did not copy null", null, err)
			}
			invokeInit(2, 0xfeedface00000000, 0xdeadbeef00000001)
			if null, err := table.EntryIsNull(2); err != nil || !null {
				t.Fatal("table64.init did not zero-normalize i32 carriers", null, err)
			}
			before := slices.Clone(table.desc)
			for _, invalid := range [][3]uint64{{1 << 32, 0, 0}, {0, 1, 1}, {0, 0, 2}, {^uint64(0), 0, 2}} {
				got, err := in.Invoke("init", invalid[0], invalid[1], invalid[2])
				var trap *coreruntime.TrapError
				if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
					t.Fatal("table64.init range did not trap", invalid, got, err)
				}
				if !slices.Equal(table.desc, before) || table.Size() != 4 {
					t.Fatal("table64.init trap changed storage", invalid)
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				if got, err := in.Invoke("drop"); err != nil || !slices.Equal(got, []uint64{1}) || !slices.Equal(table.desc, before) {
					t.Fatal("table64.elem.drop repeat", attempt, got, err)
				}
			}
			invokeInit(4, 0, 0)
			got, err := in.Invoke("init", 0, 0, 1)
			var trap *coreruntime.TrapError
			if err == nil || got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || !slices.Equal(table.desc, before) {
				t.Fatal("table64.init used dropped segment", got, err)
			}
			clear(table.desc[8:])
		})
	}
}

func TestRV64PublicOwnedTable64InitDropArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-init-drop-admission", rv64Table64InitDropInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"copy-witness":    func(v *Compiled) { v.nativeTable64ABI = nativeTable64CopyABI },
		"dest-param":      func(v *Compiled) { v.Funcs[0].Params[0] = ValI32 },
		"source-param":    func(v *Compiled) { v.Funcs[0].Params[1] = ValI64 },
		"count-param":     func(v *Compiled) { v.Funcs[0].Params[2] = ValI64 },
		"drop-result":     func(v *Compiled) { v.Funcs[1].Results[0] = ValI64 },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"missing-bulk":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"no-max":          func(v *Compiled) { v.TableHasMax = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 10)
		},
		"extra-function": func(v *Compiled) { v.Funcs = append(v.Funcs, FuncSig{}) },
		"active-element": func(v *Compiled) { v.passiveElems[0].Mode = ElemModeActive },
		"nonnull-element": func(v *Compiled) {
			v.passiveElems[0].Values[0].Null = false
		},
		"table32": func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			for i := range bad.Funcs {
				bad.Funcs[i].Params = slices.Clone(bad.Funcs[i].Params)
				bad.Funcs[i].Results = slices.Clone(bad.Funcs[i].Results)
			}
			bad.Exports = maps.Clone(bad.Exports)
			bad.passiveElems = slices.Clone(bad.passiveElems)
			bad.passiveElems[0].Values = slices.Clone(bad.passiveElems[0].Values)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64.init/drop metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if name == "missing-ref" || name == "missing-bulk" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
					t.Fatal("serialization did not restore structural element features", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid table64.init/drop artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTable64IndirectArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-indirect-admission", rv64Table64IndirectInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness":  func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"getset-witness":   func(v *Compiled) { v.nativeTable64ABI = nativeTable64GetSetABI },
		"target-result":    func(v *Compiled) { v.Funcs[0].Results[0] = ValI64 },
		"caller-param":     func(v *Compiled) { v.Funcs[1].Params[0] = ValI32 },
		"missing-ref":      func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"spurious-private": func(v *Compiled) { v.nativeReferenceFunctionABI = true },
		"missing-descs":    func(v *Compiled) { v.NeedsFuncRefDescs = false },
		"no-max":           func(v *Compiled) { v.TableHasMax = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 13)
		},
		"extra-function": func(v *Compiled) { v.Funcs = append(v.Funcs, FuncSig{}) },
		"active-element": func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32":        func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			for i := range bad.Funcs {
				bad.Funcs[i].Params = slices.Clone(bad.Funcs[i].Params)
				bad.Funcs[i].Results = slices.Clone(bad.Funcs[i].Results)
			}
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64 indirect metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid table64 indirect artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTable64GetSetArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-get-set-admission", rv64Table64GetSetInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"grow-witness":    func(v *Compiled) { v.nativeTable64ABI = nativeTable64GrowABI },
		"param":           func(v *Compiled) { v.Funcs[0].Params[0] = ValI32 },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI64 },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"no-max":          func(v *Compiled) { v.TableHasMax = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 14)
		},
		"extra-function": func(v *Compiled) {
			v.Funcs = append(v.Funcs, FuncSig{})
		},
		"element": func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32": func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64 get/set metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid table64 get/set artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTable64GrowArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-grow-admission", rv64Table64GrowInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"size-witness":    func(v *Compiled) { v.nativeTable64ABI = nativeTable64SizeABI },
		"param":           func(v *Compiled) { v.Funcs[0].Params[0] = ValI32 },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI32 },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"no-max":          func(v *Compiled) { v.TableHasMax = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 15)
		},
		"extra-function": func(v *Compiled) {
			v.Funcs = append(v.Funcs, FuncSig{})
		},
		"element": func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32": func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64.grow metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid table64.grow artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTable64SizeArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-size-admission", rv64Table64SizeInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI32 },
		"export":          func(v *Compiled) { v.Exports["size"] = 1 },
		"extra-function": func(v *Compiled) {
			v.Funcs = append(v.Funcs, FuncSig{})
		},
		"element": func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32": func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64.size metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid table64.size artifact admitted")
			}
		})
	}

	relabelled := rv64TableStageCompile(t, "ordinary-table32-i64", rv64RelabelledTable32FunctionSource(), false)
	if relabelled.nativeTable64ABI != nativeTable64NoABI {
		t.Fatal("ordinary table32 function acquired table64.size compiler witness")
	}
	bad := *relabelled.executionView()
	bad.TableAddr64 = true
	bad.requiredFeatures |= CoreFeatureTable64
	if err := bad.validate(); err == nil {
		t.Fatal("ordinary table32 function relabelled as table64.size")
	}
	blob, err := marshalCompiled(&bad)
	if err == nil {
		loaded, loadErr := LoadTrustedArtifact(blob)
		if loaded != nil {
			_ = loaded.Close()
		}
		if loadErr == nil {
			t.Fatal("ordinary table32 artifact relabelled as table64.size")
		}
	}
}

func TestRV64PublicImportedTable64SizeArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "imported-table64-size-admission", rv64ImportedTable64SizeInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"owned-witness":   func(v *Compiled) { v.nativeTable64ABI = nativeTable64SizeABI },
		"missing-import":  func(v *Compiled) { v.tableImport = "" },
		"missing-max":     func(v *Compiled) { v.tableImportHasMax = false },
		"minimum":         func(v *Compiled) { v.tableImportMin = 1025 },
		"maximum":         func(v *Compiled) { v.tableImportMax = 1025 },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI32 },
		"export":          func(v *Compiled) { v.Exports["size"] = 1 },
		"table32":         func(v *Compiled) { v.TableAddr64 = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 9)
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid imported table64.size metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid imported table64.size artifact admitted")
			}
		})
	}
}

func TestRV64PublicImportedTable64GetSetArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "imported-table64-getset-admission", rv64ImportedTable64GetSetInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"owned-witness":   func(v *Compiled) { v.nativeTable64ABI = nativeTable64GetSetABI },
		"size-witness":    func(v *Compiled) { v.nativeTable64ABI = nativeTable64ImportedSizeABI },
		"missing-import":  func(v *Compiled) { v.tableImport = "" },
		"missing-max":     func(v *Compiled) { v.tableImportHasMax = false },
		"minimum":         func(v *Compiled) { v.tableImportMin = 1025 },
		"maximum":         func(v *Compiled) { v.tableImportMax = 1025 },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"param":           func(v *Compiled) { v.Funcs[0].Params[0] = ValI32 },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI64 },
		"export":          func(v *Compiled) { v.Exports["roundtrip"] = 1 },
		"table32":         func(v *Compiled) { v.TableAddr64 = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 8)
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid imported table64.get/set metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid imported table64.get/set artifact admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyTable64SourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source admission only")
	}
	source := rv64StateOnlyTable64Source(0, 4, "table")
	if c, err := NewRuntimeConfig().WithCoreFeatures(platformCoreFeatures() &^ CoreFeatureTable64).Compile(source); c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("disabled table64 feature admitted", err)
	}
	secondTable := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0, 4}, []byte{0x70, 0x05, 0, 4})))
	withMemory := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0, 4})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 0})))
	imported := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 0, 4)
	importedTable := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imported)))
	overCapacity := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0, 0x81, 0x08})))
	for name, bad := range map[string][]byte{
		"instruction": rv64Table64UnsupportedInstructionSource(),
		"grow-no-max": rv64Table64GrowNoMaxSource(),
		"second":      secondTable,
		"memory":      withMemory,
		"imported":    importedTable,
		"capacity":    overCapacity,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, bad)
			if c != nil {
				_ = c.Close()
			}
			if err == nil {
				t.Fatal("unsupported table64 source admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyTable64ArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-table64-admission", rv64StateOnlyTable64Source(0, 4, "table"), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-feature":            func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTable64 },
		"contradictory-size-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64SizeABI },
		"contradictory-grow-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64GrowABI },
		"contradictory-getset-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64GetSetABI
		},
		"contradictory-indirect-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64IndirectABI
		},
		"contradictory-fill-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64FillABI
		},
		"contradictory-copy-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64CopyABI
		},
		"contradictory-init-drop-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64InitDropABI
		},
		"contradictory-imported-size-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64ImportedSizeABI
		},
		"contradictory-imported-getset-witness": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64ImportedGetSetABI
		},
		"table32":     func(v *Compiled) { v.TableAddr64 = false },
		"start":       func(v *Compiled) { v.HasStart = true },
		"initializer": func(v *Compiled) { v.HasTableInitFunc = true },
		"element":     func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"passive":     func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModePassive}} },
		"externref":   func(v *Compiled) { v.TableType = ValExternRef },
		"capacity":    func(v *Compiled) { v.TableMax = 1025 },
		"extra-table": func(v *Compiled) {
			v.extraTables = append(slices.Clone(v.extraTables), tableDef{Type: ValFuncRef})
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Elems = slices.Clone(bad.Elems)
			bad.passiveElems = slices.Clone(bad.passiveElems)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64 metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if name == "missing-feature" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureTable64) {
					t.Fatal("serialization did not restore structural table64 feature", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid table64 artifact admitted")
			}
		})
	}
	if c.codeCache.refs != 0 || c.codeCache.mem != nil || c.codeCache.base != 0 || c.codeCache.sealed {
		t.Fatal("table64 metadata gates reached executable publication")
	}
}
