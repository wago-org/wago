//go:build linux && amd64 && (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_stride))

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func compactFillModule(storage, value, get byte) []byte {
	return compactFillSizedModule(storage, value, get, 3)
}
func compactFillSizedModule(storage, value, get, length byte) []byte {
	init := []byte{0x41, length, 0xfb, 7, 0, 0x24, 0, 0x0b}
	fill := []byte{0x23, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 16, 0, 0x0b}
	read := []byte{0x23, 0, 0x20, 0, 0xfb, get, 0, 0x0b}
	churn := []byte{0xfb, 1, 1, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1}, []byte{0x5f, 0}, []byte{0x60, 0, 0}, []byte{0x60, 3, 0x7f, value, 0x7f, 0}, []byte{0x60, 1, 0x7f, 1, value})),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3}, []byte{4}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("fill", 0, 1), wasmtest.ExportEntry("get", 0, 2), wasmtest.ExportEntry("churn", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(fill), wasmtest.Code(read), wasmtest.Code(churn))))
}
func TestCompactNativeGCFillShapesAndBounds(t *testing.T) {
	for _, tc := range []struct {
		s, v, g    byte
		bits, want uint64
	}{
		{0x78, 0x7f, 12, 255, 0xffffffff}, {0x78, 0x7f, 13, 255, 255},
		{0x77, 0x7f, 12, 65535, 0xffffffff}, {0x7f, 0x7f, 11, 0xdeadbeef, 0xdeadbeef},
		{0x7e, 0x7e, 11, 0x123456789abcdef, 0x123456789abcdef},
		{0x7d, 0x7d, 11, 0x7fc12345, 0x7fc12345}, {0x7c, 0x7c, 11, 0x8000000000000000, 0x8000000000000000},
	} {
		t.Run(fmt.Sprintf("%x/%d", tc.s, tc.g), func(t *testing.T) {
			in := newCompactScalarArrayInstance(t, compactFillModule(tc.s, tc.v, tc.g))
			if _, e := in.Invoke("fill", 0, tc.bits, 0); e == nil {
				t.Fatal("null zero fill accepted")
			}
			if _, e := in.Invoke("init"); e != nil {
				t.Fatal(e)
			}
			const sentinel = 0x81234567
			for round := 0; round < 3; round++ {
				binary.LittleEndian.PutUint32(in.ctrl[runtime.HostCtrlImportIndexOffset:], sentinel)
				if _, e := in.Invoke("fill", uint64(1)<<32, tc.bits, 3|(uint64(1)<<32)); e != nil {
					t.Fatal(e)
				}
				if binary.LittleEndian.Uint32(in.ctrl[runtime.HostCtrlImportIndexOffset:]) != sentinel {
					t.Fatal("fill used helper")
				}
				for i := 0; i < 3; i++ {
					got, e := in.Invoke("get", uint64(i))
					if e != nil || len(got) != 1 || got[0] != tc.want {
						t.Fatal(got, e)
					}
				}
				for _, span := range [][2]uint64{{3, 0}, {0, 0}} {
					if _, e := in.Invoke("fill", span[0], 0, span[1]); e != nil {
						t.Fatal(e)
					}
				}
				for _, span := range [][2]uint64{{4, 0}, {3, 1}, {2, 2}, {0xffffffff, 1}, {1, 0xffffffff}} {
					if _, e := in.Invoke("fill", span[0], 0, span[1]); e == nil {
						t.Fatal("invalid span accepted", span)
					}
				}
				for i := 0; i < 3; i++ {
					got, e := in.Invoke("get", uint64(i))
					if e != nil || got[0] != tc.want {
						t.Fatal("failed fill mutated payload", got, e)
					}
				}
				if _, e := in.Invoke("churn"); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestCompactNativeGCFillTrapAndPhysicalExtent(t *testing.T) {
	data := compactFillModule(0x7e, 0x7e, 11)
	in := newCompactScalarArrayInstance(t, data)
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	want := uint32(bytes.Index(m.Code[1].BodyBytes, []byte{0xfb, 16, 0})) + m.Code[1].LocalDeclBytes
	checkTrap := func(index, count uint64) {
		t.Helper()
		_, e := in.Invoke("fill", index, 99, count)
		var trap *TrapError
		if !errors.As(e, &trap) || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != want {
			t.Fatalf("trap %v frames %+v want PC %d", e, trap.Frames, want)
		}
	}
	checkTrap(0xffffffff, 0)
	if _, e := in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	if _, e := in.Invoke("fill", 0, 77, 3); e != nil {
		t.Fatal(e)
	}
	checkTrap(2, 2)
	v := in.gc.CompactView()
	arena := unsafe.Slice((*byte)(unsafe.Pointer(v.Arena)), int(v.ArenaBytes))
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	old := binary.LittleEndian.Uint32(arena[ref+4:])
	binary.LittleEndian.PutUint32(arena[ref+4:], 0xffffffff)
	// The shared fully-checked resolver has multiple cast-failure sites, so
	// existing trap-stub grouping intentionally reports unknown PC for this
	// private-metadata error. Public null and logical errors above stay precise.
	_, err := in.Invoke("fill", 0, 99, 1)
	var physical *TrapError
	if !errors.As(err, &physical) {
		t.Fatal("missing physical span trap", err)
	}
	binary.LittleEndian.PutUint32(arena[ref+4:], old)
	for i := 0; i < 3; i++ {
		got, e := in.Invoke("get", uint64(i))
		if e != nil || got[0] != 77 {
			t.Fatal("physical trap mutated payload", got, e)
		}
	}
}
