//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
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

func rv64TwoTable32SizeInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x01, 1, 2},
			[]byte{0x70, 0x01, 3, 4},
		)),
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

func rv64TwoTable32GetSetInstructionSource() []byte {
	body := func(index byte) []byte {
		return []byte{0x20, 0, 0xd0, 0x70, 0x26, index, 0x20, 0, 0x25, index, 0xd1, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x01, 1, 2},
			[]byte{0x70, 0x01, 3, 4},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("roundtrip0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("roundtrip1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(0)), wasmtest.Code(body(1)))),
	)
}

// rv64RawTwoTable32GetSet deliberately writes the native argument cell after
// public admission so the machine-code consumer, rather than API marshaling,
// is responsible for i32 normalization.
func rv64RawTwoTable32GetSet(t *testing.T, in *Instance, name string, index uint64) (uint64, error) {
	t.Helper()
	state := in.lockInvocation(0)
	defer state.unlockInvocation()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	if err := in.bindAndValidateNativeContext(); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(in.serArgs, index)
	binary.LittleEndian.PutUint64(in.results, ^uint64(0))
	if err := refreshNativeControl(true, in.eng, in.jm, in.trap); err != nil {
		t.Fatal(err)
	}
	entry := in.base + uintptr(in.c.Entry[in.c.Exports[name]-in.c.NumImports])
	err := in.decorateTrap(in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results))
	return binary.LittleEndian.Uint64(in.results), err
}

func TestRV64PublicOwnedTwoTable32Size(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-two-table32-size", rv64TwoTable32SizeInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64SizeABI || !rv64OwnedTwoTable32Size(c) || c.tableCount() != 2 || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
				t.Fatalf("two-table32.size metadata contract: witness=%d qualified=%v tables=%d features=%#x table0=%+v table1=%+v funcs=%+v exports=%v tableExports=%v refs=%v keys=%v", c.nativeTable64ABI, rv64OwnedTwoTable32Size(c), c.tableCount(), c.requiredFeatures, c.tableDef(0), c.tableDef(1), c.Funcs, c.Exports, c.tableExports, c.NeedsFuncRefDescs, c.FuncTypeID)
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			first, second := in.tableDescriptor(0), in.tableDescriptor(1)
			if in.jm.TableDirPtr() == 0 || len(first) < 8 || len(second) < 8 || &first[0] == &second[0] {
				t.Fatal("two-table32 descriptor directory")
			}
			for range 3 {
				if got, err := in.Invoke("size0"); err != nil || !slices.Equal(got, []uint64{1}) {
					t.Fatal("table32.size table 0/zero-normalized result", got, err)
				}
				if got, err := in.Invoke("size1"); err != nil || !slices.Equal(got, []uint64{3}) {
					t.Fatal("table32.size table 1/zero-normalized result", got, err)
				}
			}
			capacity := binary.LittleEndian.Uint32(second[4:])
			binary.LittleEndian.PutUint32(second[4:], capacity+railshot.MaxOwnedTableEntries)
			got, invokeErr := in.Invoke("size0")
			binary.LittleEndian.PutUint32(second[4:], capacity)
			if invokeErr == nil || len(got) != 0 {
				t.Fatal("unselected corrupt table32 descriptor accepted", got, invokeErr)
			}
			if got, err := in.Invoke("size0"); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("table32.size reuse after descriptor restore", got, err)
			}
			table0, err := in.ExportedTable("table0")
			if err != nil || table0.Size() != 1 {
				t.Fatal("table32 table 0 export", table0, err)
			}
			t.Cleanup(func() { _ = table0.Close() })
			table1, err := in.ExportedTable("table1")
			if err != nil || table1.Size() != 3 || table1 == table0 {
				t.Fatal("table32 table 1 export", table1, err)
			}
			t.Cleanup(func() { _ = table1.Close() })
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("size1"); err != nil || !slices.Equal(got, []uint64{3}) {
				t.Fatal("table32.size table 1 after compiled close", got, err)
			}
		})
	}
}

func TestRV64PublicOwnedTwoTable32SizeArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-two-table32-size-admission", rv64TwoTable32SizeInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"table64-feature": func(v *Compiled) { v.requiredFeatures |= CoreFeatureTable64 },
		"missing-table":   func(v *Compiled) { v.extraTables = nil },
		"first-table64":   func(v *Compiled) { v.TableAddr64 = true },
		"second-table64":  func(v *Compiled) { v.extraTables[0].Addr64 = true },
		"second-no-max":   func(v *Compiled) { v.extraTables[0].HasMax = false },
		"second-initializer": func(v *Compiled) {
			v.extraTables[0].HasInitFunc = true
			v.extraTables[0].InitFunc = 1
		},
		"missing-key":     func(v *Compiled) { v.FuncTypeID = v.FuncTypeID[:1] },
		"wrong-key":       func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"second-result":   func(v *Compiled) { v.Funcs[1].Results[0] = ValI64 },
		"function-export": func(v *Compiled) { v.Exports["size1"] = 0 },
		"table-export":    func(v *Compiled) { v.tableExports["table1"] = 0 },
		"element":         func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			for i := range bad.Funcs {
				bad.Funcs[i].Params = slices.Clone(bad.Funcs[i].Params)
				bad.Funcs[i].Results = slices.Clone(bad.Funcs[i].Results)
			}
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid two-table32.size metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "missing-ref" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
					t.Fatal("structural table feature was not restored", loaded, err)
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid two-table32.size artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTwoTable32GetSet(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-two-table32-get-set", rv64TwoTable32GetSetInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64GetSetABI || !rv64OwnedTwoTable32GetSet(c) || c.tableCount() != 2 || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
				t.Fatalf("two-table32 get/set metadata contract: witness=%d qualified=%v tables=%d features=%#x", c.nativeTable64ABI, rv64OwnedTwoTable32GetSet(c), c.tableCount(), c.requiredFeatures)
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table0, err := in.ExportedTable("table0")
			if err != nil || table0.Size() != 1 {
				t.Fatal("two-table32 get/set table 0", table0, err)
			}
			t.Cleanup(func() { _ = table0.Close() })
			table1, err := in.ExportedTable("table1")
			if err != nil || table1.Size() != 3 || table1 == table0 {
				t.Fatal("two-table32 get/set table 1", table1, err)
			}
			t.Cleanup(func() { _ = table1.Close() })
			if len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
				t.Fatal("two-table32 get/set missing canonical local descriptor")
			}
			row := in.funcRefDescs[coreruntime.FuncRefDescBytes : coreruntime.FuncRefDescBytes+coreruntime.TableEntryBytes]
			copy(table0.desc[8:], row)
			copy(table1.desc[8+2*coreruntime.TableEntryBytes:], row)

			second := in.tableDescriptor(1)
			capacity := binary.LittleEndian.Uint32(second[4:])
			before0, before1 := slices.Clone(table0.desc), slices.Clone(table1.desc)
			binary.LittleEndian.PutUint32(second[4:], capacity+railshot.MaxOwnedTableEntries)
			got, invokeErr := in.Invoke("roundtrip0", 0)
			binary.LittleEndian.PutUint32(second[4:], capacity)
			if invokeErr == nil || len(got) != 0 || !slices.Equal(table0.desc, before0) || !slices.Equal(table1.desc, before1) {
				t.Fatal("unselected corrupt table32 descriptor accepted or mutated storage", got, invokeErr)
			}
			table1.desc[8+8] = 1
			got, invokeErr = in.Invoke("roundtrip0", 0)
			table1.desc[8+8] = 0
			if invokeErr == nil || len(got) != 0 || !slices.Equal(table0.desc, before0) || !slices.Equal(table1.desc, before1) {
				t.Fatal("unselected corrupt table32 null row accepted or mutated storage", got, invokeErr)
			}

			// The raw bridge deliberately supplies dirty upper halves. Both table
			// consumers must use the low i32, and the i32 result must be zeroed.
			if got, err := rv64RawTwoTable32GetSet(t, in, "roundtrip1", 0xfeed_beef_0000_0002); err != nil || got != 1 {
				t.Fatal("two-table32 get/set dirty incoming carrier", got, err)
			}
			if !slices.Equal(table0.desc[8:8+coreruntime.TableEntryBytes], row) || !slices.Equal(table1.desc[8+2*coreruntime.TableEntryBytes:8+3*coreruntime.TableEntryBytes], make([]byte, coreruntime.TableEntryBytes)) {
				t.Fatal("two-table32 get/set selected wrong descriptor")
			}
			if got, err := rv64RawTwoTable32GetSet(t, in, "roundtrip0", 1<<32); err != nil || got != 1 {
				t.Fatal("two-table32 get/set ignored-low-zero carrier", got, err)
			}

			before0, before1 = slices.Clone(table0.desc), slices.Clone(table1.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				index    uint64
			}{{"roundtrip0", 0, 1}, {"roundtrip1", 1, 3}, {"roundtrip1", 1, 0x1234_5678_ffff_ffff}} {
				got, err := rv64RawTwoTable32GetSet(t, in, tc.name, tc.index)
				var trap *coreruntime.TrapError
				if err == nil || got != ^uint64(0) || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
					t.Fatal("two-table32 get/set index did not trap", tc, got, err)
				}
				if !slices.Equal(table0.desc, before0) || !slices.Equal(table1.desc, before1) {
					t.Fatal("two-table32 get/set trap changed storage", tc)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("roundtrip1", 0); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("two-table32 get/set after compiled close", got, err)
			}
		})
	}
}

func TestRV64PublicOwnedTwoTable32GetSetArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-two-table32-get-set-admission", rv64TwoTable32GetSetInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"table64-feature": func(v *Compiled) { v.requiredFeatures |= CoreFeatureTable64 },
		"missing-table":   func(v *Compiled) { v.extraTables = nil },
		"first-table64":   func(v *Compiled) { v.TableAddr64 = true },
		"second-table64":  func(v *Compiled) { v.extraTables[0].Addr64 = true },
		"second-no-max":   func(v *Compiled) { v.extraTables[0].HasMax = false },
		"second-initializer": func(v *Compiled) {
			v.extraTables[0].HasInitFunc = true
			v.extraTables[0].InitFunc = 1
		},
		"missing-key":     func(v *Compiled) { v.FuncTypeID = v.FuncTypeID[:1] },
		"wrong-key":       func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"second-param":    func(v *Compiled) { v.Funcs[1].Params[0] = ValI64 },
		"second-result":   func(v *Compiled) { v.Funcs[1].Results[0] = ValI64 },
		"function-export": func(v *Compiled) { v.Exports["roundtrip1"] = 0 },
		"table-export":    func(v *Compiled) { v.tableExports["table1"] = 0 },
		"element":         func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			for i := range bad.Funcs {
				bad.Funcs[i].Params = slices.Clone(bad.Funcs[i].Params)
				bad.Funcs[i].Results = slices.Clone(bad.Funcs[i].Results)
			}
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid two-table32 get/set metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "missing-ref" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
					t.Fatal("structural table feature was not restored", loaded, err)
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid two-table32 get/set artifact admitted")
			}
		})
	}
}
