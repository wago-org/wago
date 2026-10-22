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

func privateReferenceCode(locals, body []byte) []byte {
	fn := append(slices.Clone(locals), body...)
	return append(wasmtest.ULEB(uint32(len(fn))), fn...)
}

func privateReferenceModule() []byte {
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
	}
	// All boundary types remain numeric; only locals and internal operands are
	// nullable funcref. Targets 0/1 are declared through the active element.
	funcs := []byte{13, 0, 0, 1, 2, 3, 3, 4, 0, 0, 2, 0, 0, 0}
	names := []string{"target-a", "target-b", "call", "size", "clear", "set-a", "copy", "is-null", "hold", "init-local", "join", "below-calls", "below-loop"}
	var exports [][]byte
	for i := 2; i < len(names); i++ {
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
	}
	locals := []byte{1, 1, 0x70}
	// 254 I64 locals plus a funcref force the real large SP displacement path.
	largeLocals := []byte{2, 0xfe, 1, 0x7e, 1, 0x70}
	body := [][]byte{
		wasmtest.Code([]byte{0x20, 0, 0x41, 7, 0x6a, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x41, 9, 0x6a, 0x0b}),
		wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x11, 0, 0, 0x0b}),
		wasmtest.Code([]byte{0xfc, 16, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x26, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x25, 0, 0x26, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0xd1, 0x0b}),
		privateReferenceCode(locals, []byte{
			0x41, 0, 0x25, 0, 0x22, 1, 0x1a, // retain canonical handle
			0x41, 0, 0xd0, 0x70, 0x26, 0, // replace source with null
			0x41, 2, 0x20, 1, 0xd2, 1, 0x20, 0, 0x1c, 1, 0x70, 0x26, 0,
			0x41, 5, 0x41, 2, 0x11, 0, 0, 0x0b,
		}),
		privateReferenceCode(locals, []byte{0x20, 0, 0xd1, 0x0b}),
		privateReferenceCode(largeLocals, []byte{
			0x20, 0, 0x04, 0x40, 0xd2, 0, 0x21, 0xff, 1,
			0x05, 0xd2, 1, 0x21, 0xff, 1, 0x0b,
			0x41, 2, 0x20, 0xff, 1, 0x26, 0,
			0x41, 5, 0x41, 2, 0x11, 0, 0, 0x0b,
		}),
		privateReferenceCode(locals, []byte{
			0xd2, 0, // a reference stays below both callee result vectors
			0x20, 0, 0x04, 0x40, 0x01, 0x05, 0x01, 0x0b,
			0x41, 5, 0x10, 0, 0x1a,
			0x41, 5, 0x41, 1, 0x11, 0, 0, 0x1a,
			0x21, 1, 0x41, 2, 0x20, 1, 0x26, 0,
			0x41, 5, 0x41, 2, 0x11, 0, 0, 0x0b,
		}),
		privateReferenceCode(locals, []byte{
			0xd2, 0, 0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x04, 0x40,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 1,
			0x0b, 0x0b, 0x0b,
			0x21, 1, 0x41, 2, 0x20, 1, 0x26, 0,
			0x41, 5, 0x41, 2, 0x11, 0, 0, 0x0b,
		}),
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, funcs), ownedTableDeclaration(3), wasmtest.Section(7, wasmtest.Vec(exports...)), ownedTableElements(0, 0, 1), wasmtest.Section(10, wasmtest.Vec(body...)))
}

func TestPublicPrivateReferenceMutationIdentityLocalsAndArtifact(t *testing.T) {
	c := ownedTableCompile(t, privateReferenceModule())
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
	importedGlobalInvoke(t, in, "size", []uint64{3})
	importedGlobalInvoke(t, in, "init-local", []uint64{1})
	importedGlobalInvoke(t, in, "is-null", []uint64{1}, 0xcafebabe00000002)
	importedGlobalInvoke(t, in, "copy", nil, 0xcafebabe00000002, 0xfeedface00000000)
	importedGlobalInvoke(t, in, "call", []uint64{12}, 2, 0xfeedface00000005)
	importedGlobalInvoke(t, in, "clear", nil, 2)
	table := unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
	if !slices.Equal(table[8+2*32:8+3*32], make([]byte, 32)) {
		t.Fatal("null overwrite retained entry fields")
	}
	ownedTableTrap(t, invokePrivateReference(t, in, "call", 2, 5), coreruntime.TrapIndirectOutOfBounds)
	importedGlobalInvoke(t, in, "hold", []uint64{12}, 0xfeedface00000001)
	importedGlobalInvoke(t, in, "is-null", []uint64{1}, 0)
	importedGlobalInvoke(t, in, "set-a", nil, 0)
	importedGlobalInvoke(t, in, "hold", []uint64{14}, 0)
	importedGlobalInvoke(t, in, "join", []uint64{12}, 0xfeedface00000001)
	importedGlobalInvoke(t, in, "join", []uint64{14}, 0)
	importedGlobalInvoke(t, in, "set-a", nil, 0)
	importedGlobalInvoke(t, in, "below-calls", []uint64{12}, 0)
	importedGlobalInvoke(t, in, "below-calls", []uint64{12}, 1)
	importedGlobalInvoke(t, in, "below-loop", []uint64{12}, 0)
	importedGlobalInvoke(t, in, "below-loop", []uint64{12}, 7)
	fn, err := in.WasmFunc("copy")
	if err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() {
		if _, err := fn.Invoke(2, 0); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm private mutation allocations", n)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if in.hasPhysicalResources() {
		t.Fatal("private references retained final owners")
	}
}

func invokePrivateReference(t *testing.T, in *Instance, name string, args ...uint64) error {
	t.Helper()
	_, err := in.Invoke(name, args...)
	return err
}

func TestPublicPrivateReferenceBoundsBeforeMutationAndReuse(t *testing.T) {
	c := ownedTableCompile(t, privateReferenceModule())
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	table := unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
	saved := slices.Clone(table)
	for _, tc := range []struct {
		name string
		args []uint64
		pc   uint32
	}{
		{"clear", []uint64{3}, 5}, {"set-a", []uint64{0xffffffffffffffff}, 5},
		{"copy", []uint64{3, 0}, 7}, {"copy", []uint64{0, 3}, 5}, {"is-null", []uint64{3}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trap := ownedTableTrap(t, invokePrivateReference(t, in, tc.name, tc.args...), coreruntime.TrapTableOutOfBounds)
			if runtime.GOARCH == "riscv64" && (len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != tc.pc) {
				t.Fatal("table source frame", trap.Frames)
			}
			if !slices.Equal(table, saved) || binary.LittleEndian.Uint32(table) != 3 {
				t.Fatal("failing operation mutated table")
			}
			importedGlobalInvoke(t, in, "call", []uint64{12}, 0, 5)
		})
	}
}

func TestPublicPrivateReferenceRequiresReferenceTypes(t *testing.T) {
	c, err := Compile(ownedTableConfig().WithFeature(CoreFeatureReferenceTypes, false), privateReferenceModule())
	if c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("private reference feature disable ignored", err)
	}
}
