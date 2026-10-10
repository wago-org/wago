//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid two-table32.size artifact admitted")
			}
		})
	}
}
