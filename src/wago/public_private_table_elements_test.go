//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

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

func privateLegacyElement(mode byte, functions ...uint32) []byte {
	out := []byte{mode}
	if mode == 0 {
		out = append(out, 0x41, 0, 0x0b)
	} else {
		out = append(out, 0)
	}
	out = append(out, wasmtest.ULEB(uint32(len(functions)))...)
	for _, f := range functions {
		out = append(out, wasmtest.ULEB(f)...)
	}
	return out
}

func privateTableElementsModule() []byte {
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType(nil, nil),
	}
	funcs := []byte{13, 0, 0, 1, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3}
	names := []string{"a", "b", "call", "init-active", "init-declarative", "init-passive", "init-empty-active", "init-last", "drop-active", "drop-declarative", "drop-passive", "drop-empty-active", "drop-last"}
	var exports [][]byte
	for i := 2; i < len(names); i++ {
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
	}
	bodies := [][]byte{wasmtest.Code([]byte{0x20, 0, 0x41, 7, 0x6a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 9, 0x6a, 0x0b}), wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x11, 0, 0, 0x0b})}
	for _, index := range []uint32{0, 1, 2, 3, 255} {
		body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 12}
		body = append(body, wasmtest.ULEB(index)...)
		body = append(body, 0, 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}
	for _, index := range []uint32{0, 1, 2, 3, 255} {
		body := append([]byte{0xfc, 13}, wasmtest.ULEB(index)...)
		body = append(body, 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}
	elements := make([][]byte, 256)
	elements[0] = privateLegacyElement(0, 0)
	elements[1] = privateLegacyElement(3, 1)
	elements[2] = privateLegacyElement(1, 1, 0)
	elements[3] = privateLegacyElement(0)
	for i := 4; i < 255; i++ {
		elements[i] = privateLegacyElement(3)
	}
	elements[255] = privateLegacyElement(1, 1)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, funcs), ownedTableDeclaration(4), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(9, wasmtest.Vec(elements...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestPublicPrivateTableElementsIndicesDropAndArtifact(t *testing.T) {
	c := ownedTableCompile(t, privateTableElementsModule())
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	first, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = c.Close()
	_ = loaded.Close()
	runtime.GC()
	if len(first.c.passiveElems) != 256 || first.c.passiveElems[0].Mode != ElemModeActive || len(first.c.passiveElems[0].Values) != 0 || first.c.passiveElems[1].Mode != ElemModeDeclarative || len(first.c.passiveElems[1].Values) != 0 {
		t.Fatal("original dropped segment index layout")
	}
	importedGlobalInvoke(t, first, "init-passive", nil, 0xfeedface00000001, 0xdeadbeef00000000, 0xcafebabe00000002)
	assertPrivateTableTargets(t, first, 12, 14, 12, 0)
	importedGlobalInvoke(t, first, "init-last", nil, 3, 0, 1)
	assertPrivateTableTargets(t, first, 12, 14, 12, 14)
	directory := unsafe.Slice((*byte)(offHeapPtr(first.jm.PassiveElemPtr())), 256*16)
	binary.LittleEndian.PutUint32(directory[2*16+12:], 0x11223344)
	pointer := binary.LittleEndian.Uint64(directory[2*16:])
	importedGlobalInvoke(t, first, "drop-passive", nil)
	if binary.LittleEndian.Uint64(directory[2*16:]) != pointer || binary.LittleEndian.Uint32(directory[2*16+8:]) != 0 || binary.LittleEndian.Uint32(directory[2*16+12:]) != 0x11223344 {
		t.Fatal("drop modified descriptor pointer/pad")
	}
	importedGlobalInvoke(t, first, "drop-passive", nil)
	ownedTableTrap(t, invokePrivateReference(t, first, "init-passive", 0, 0, 1), coreruntime.TrapTableOutOfBounds)
	importedGlobalInvoke(t, second, "init-passive", nil, 1, 0, 2)
	assertPrivateTableTargets(t, second, 12, 14, 12, 0)
	fn, err := second.WasmFunc("init-last")
	if err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() {
		if _, err := fn.Invoke(3, 0, 1); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm table.init allocation", n)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	assertPrivateTableTargets(t, second, 12, 14, 12, 14)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if second.hasPhysicalResources() {
		t.Fatal("element roots retained")
	}
}

func TestPublicPrivateTableElementsCompleteBoundsBeforeWrites(t *testing.T) {
	c := ownedTableCompile(t, privateTableElementsModule())
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
		{"init-passive", []uint64{3, 0, 2}}, {"init-passive", []uint64{0, 1, 2}}, {"init-passive", []uint64{0, 3, 0}}, {"init-passive", []uint64{5, 0, 0}},
		{"init-active", []uint64{0, 0, 1}}, {"init-declarative", []uint64{0, 0, 1}}, {"init-empty-active", []uint64{0, 1, 0}},
		{"init-passive", []uint64{0xffffffff, 0xffffffff, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trap := ownedTableTrap(t, invokePrivateReference(t, in, tc.name, tc.args...), coreruntime.TrapTableOutOfBounds)
			if runtime.GOARCH == "riscv64" && (len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7) {
				t.Fatal("table.init source frame", trap.Frames)
			}
			if !slices.Equal(before, privateTableBytes(in)) {
				t.Fatal("table.init wrote before range trap")
			}
			assertPrivateTableTargets(t, in, 12, 0, 0, 0)
		})
	}
	for _, name := range []string{"init-active", "init-declarative", "init-empty-active"} {
		importedGlobalInvoke(t, in, name, nil, 4, 0, 0)
	}
	importedGlobalInvoke(t, in, "init-passive", nil, 4, 2, 0)
	for _, name := range []string{"drop-active", "drop-declarative", "drop-empty-active", "drop-passive", "drop-last"} {
		importedGlobalInvoke(t, in, name, nil)
		importedGlobalInvoke(t, in, name, nil)
	}
	importedGlobalInvoke(t, in, "init-passive", nil, 4, 0, 0)
	ownedTableTrap(t, invokePrivateReference(t, in, "init-passive", 4, 1, 0), coreruntime.TrapTableOutOfBounds)
	if !slices.Equal(before, privateTableBytes(in)) {
		t.Fatal("drop/zero-length changed table")
	}
}

func TestPublicPrivateTableElementsRequireBulkFeature(t *testing.T) {
	c, err := Compile(ownedTableConfig().WithFeature(CoreFeatureBulkMemoryOperations, false), privateTableElementsModule())
	if c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("element bulk feature disable ignored", err)
	}
}
