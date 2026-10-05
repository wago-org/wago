//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func privateTableBulkModule(hasMax bool) []byte { return privateTableBulkSizedModule(hasMax, 4, 8) }

func privateTableBulkSizedModule(hasMax bool, minimum, maximum uint32) []byte {
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
	}
	declaration := ownedTableDeclaration(minimum)
	if hasMax {
		limits := append([]byte{1, 0x70, 1}, wasmtest.ULEB(minimum)...)
		limits = append(limits, wasmtest.ULEB(maximum)...)
		declaration = wasmtest.Section(4, limits)
	}
	funcs := []byte{11, 0, 0, 1, 2, 3, 3, 3, 4, 0, 0, 1}
	names := []string{"a", "b", "call", "size", "fill-a", "fill-b", "fill-null", "copy", "grow-a", "grow-null", "grow-from"}
	var exports [][]byte
	for i := 2; i < len(names); i++ {
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
	}
	body := [][]byte{
		wasmtest.Code([]byte{0x20, 0, 0x41, 7, 0x6a, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x41, 9, 0x6a, 0x0b}),
		wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x11, 0, 0, 0x0b}),
		wasmtest.Code([]byte{0xfc, 16, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x20, 1, 0xfc, 17, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xd2, 1, 0x20, 1, 0xfc, 17, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 17, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 14, 0, 0, 0x0b}),
		wasmtest.Code([]byte{0xd2, 0, 0x20, 0, 0xfc, 15, 0, 0x0b}),
		wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 15, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0x20, 1, 0xfc, 15, 0, 0x0b}),
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, funcs), declaration, wasmtest.Section(7, wasmtest.Vec(exports...)), ownedTableElements(0, 0, 1), wasmtest.Section(10, wasmtest.Vec(body...)))
}

func privateTableBytes(in *Instance) []byte {
	return unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
}
func assertPrivateTableTargets(t *testing.T, in *Instance, expected ...uint64) {
	t.Helper()
	for i, v := range expected {
		if v == 0 {
			ownedTableTrap(t, invokePrivateReference(t, in, "call", uint64(i), 5), coreruntime.TrapIndirectOutOfBounds)
		} else {
			importedGlobalInvoke(t, in, "call", []uint64{v}, uint64(i), 5)
		}
	}
}

func TestPublicPrivateTableBulkOverlapNullAndGrowthArtifact(t *testing.T) {
	c := ownedTableCompile(t, privateTableBulkModule(true))
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_ = loaded.Close()
	_ = c.Close()
	runtime.GC()
	importedGlobalInvoke(t, in, "fill-b", nil, 0xfeedface00000003, 0xdeadbeef00000001)
	assertPrivateTableTargets(t, in, 12, 14, 0, 14)
	importedGlobalInvoke(t, in, "copy", nil, 1, 0, 3)
	assertPrivateTableTargets(t, in, 12, 12, 14, 0)
	importedGlobalInvoke(t, in, "copy", nil, 0, 1, 3)
	assertPrivateTableTargets(t, in, 12, 14, 0, 0)
	importedGlobalInvoke(t, in, "copy", nil, 0, 0, 4)
	importedGlobalInvoke(t, in, "fill-a", nil, 0, 4)
	assertPrivateTableTargets(t, in, 12, 12, 12, 12)
	importedGlobalInvoke(t, in, "fill-null", nil, 1, 2)
	assertPrivateTableTargets(t, in, 12, 0, 0, 12)
	if !slices.Equal(privateTableBytes(in)[40:104], make([]byte, 64)) {
		t.Fatal("null fill left descriptor words")
	}
	before := slices.Clone(privateTableBytes(in))
	importedGlobalInvoke(t, in, "grow-a", []uint64{4}, 0)
	if !slices.Equal(before, privateTableBytes(in)) {
		t.Fatal("grow0 mutated storage")
	}
	importedGlobalInvoke(t, in, "grow-a", []uint64{0xffffffff}, 0xffffffffffffffff)
	if !slices.Equal(before, privateTableBytes(in)) {
		t.Fatal("failed growth mutated storage")
	}
	importedGlobalInvoke(t, in, "grow-a", []uint64{4}, 0xfeedface00000002)
	importedGlobalInvoke(t, in, "size", []uint64{6})
	assertPrivateTableTargets(t, in, 12, 0, 0, 12, 12, 12)
	importedGlobalInvoke(t, in, "grow-null", []uint64{6}, 2)
	importedGlobalInvoke(t, in, "size", []uint64{8})
	assertPrivateTableTargets(t, in, 12, 0, 0, 12, 12, 12, 0, 0)
	before = slices.Clone(privateTableBytes(in))
	importedGlobalInvoke(t, in, "grow-a", []uint64{0xffffffff}, 1)
	if !slices.Equal(before, privateTableBytes(in)) {
		t.Fatal("maximum failure mutated table")
	}
	fn, err := in.WasmFunc("fill-a")
	if err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() {
		if _, err := fn.Invoke(0, 8); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm bulk allocations", n)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if in.hasPhysicalResources() {
		t.Fatal("bulk table owner retained")
	}
}

func TestPublicPrivateTableBulkAllRangesBeforeWritesAndReuse(t *testing.T) {
	c := ownedTableCompile(t, privateTableBulkModule(true))
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	before := slices.Clone(privateTableBytes(in))
	for _, tc := range []struct {
		name string
		args []uint64
	}{
		{"fill-a", []uint64{3, 2}}, {"fill-null", []uint64{0, 0xffffffff}}, {"fill-b", []uint64{5, 0}},
		{"copy", []uint64{3, 0, 2}}, {"copy", []uint64{0, 3, 2}}, {"copy", []uint64{0, 5, 0}}, {"copy", []uint64{5, 0, 0}},
		{"copy", []uint64{0xffffffff, 0xffffffff, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ownedTableTrap(t, invokePrivateReference(t, in, tc.name, tc.args...), coreruntime.TrapTableOutOfBounds)
			if !slices.Equal(before, privateTableBytes(in)) {
				t.Fatal("partial writes before range trap")
			}
			assertPrivateTableTargets(t, in, 12, 14, 0, 0)
		})
	}
	importedGlobalInvoke(t, in, "fill-a", nil, 4, 0)
	importedGlobalInvoke(t, in, "copy", nil, 4, 4, 0)
	if !slices.Equal(before, privateTableBytes(in)) {
		t.Fatal("zero length mutated entries")
	}
}

func TestPublicPrivateTableNoMaxBoundedReserveAndReferenceGrowth(t *testing.T) {
	c := ownedTableCompile(t, privateTableBulkModule(false))
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	table := privateTableBytes(in)
	if binary.LittleEndian.Uint32(table[4:]) != 1024 {
		t.Fatal("bounded no-max reserve", binary.LittleEndian.Uint32(table[4:]))
	}
	importedGlobalInvoke(t, in, "grow-from", []uint64{4}, 1, 2)
	assertPrivateTableTargets(t, in, 12, 14, 0, 0, 14, 14)
	importedGlobalInvoke(t, in, "grow-null", []uint64{6}, 1018)
	importedGlobalInvoke(t, in, "size", []uint64{1024})
	importedGlobalInvoke(t, in, "grow-null", []uint64{0xffffffff}, 1)
}

func TestPublicPrivateTableBulkFeatureDisable(t *testing.T) {
	c, err := Compile(ownedTableConfig().WithFeature(CoreFeatureBulkMemoryOperations, false), privateTableBulkModule(true))
	if c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("table bulk feature disable ignored", err)
	}
}
