//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcDefinedTrapPinsModule() []byte {
	globals := make([][]byte, 0, 9)
	for range 8 {
		globals = append(globals, wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}))
	}
	globals = append(globals, []byte{0x6d, 1, 0xd0, 0x6d, 0x0b})
	bodies := make([][]byte, 2, 3)
	for i := range bodies {
		body := []byte{0x03, 0x40} // one call-free loop iteration makes globals hot
		for g := byte(0); g < 8; g++ {
			body = append(body, 0x23, g, 0x42, g+1, 0x7c, 0x24, g)
		}
		body = append(body, 0x0b, 0x23, 8)
		if i == 0 {
			body = append(body, 0xfb, 20, 0)
		} else {
			body = append(body, 0xfb, 22, 0, 0x1a, 0x41, 1)
		}
		body = append(body, 0x0b)
		bodies[i] = wasmtest.Code(body)
	}
	bodies = append(bodies, wasmtest.Code([]byte{0xfb, 1, 0, 0x24, 8, 0x41, 0, 0x0b})) // observable unused allocator requires exact roots
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5f, 1, 0x7e, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("test", 0, 0), wasmtest.ExportEntry("cast", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCDefinedTrapPreservesGlobalPins(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDefinedTrapPinsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if a := c.GCNativeRootAdmission(); !a.Required || !a.Exact {
		t.Fatalf("fixture requires exact native roots: %+v", a)
	}
	// Neither function has parameters or locals. Thus the resolver's saved
	// R9/R10/R11 are global value pins, not unused register saves or local pins.
	saves := []byte{0x41, 0x51, 0x41, 0x52, 0x41, 0x53}
	if n := bytes.Count(c.code, saves); n != 2 {
		t.Fatalf("resolver R9/R10/R11 save sequences = %d, want 2; fixture must exercise all three global pins", n)
	}
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for ai, artifact := range []*Compiled{c, loaded} {
		for _, export := range []string{"test", "cast"} {
			for _, kind := range []string{"wrong", "null", "valid", "space", "size"} {
				t.Run(fmt.Sprintf("artifact%d/%s/%s", ai, export, kind), func(t *testing.T) {
					in, err := Instantiate(artifact)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if kind != "null" {
						typeIndex := uint32(0)
						if kind == "wrong" {
							typeIndex = 1
						}
						ref, err := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(typeIndex), gc.EmptyRoots{})
						if err != nil {
							t.Fatal(err)
						}
						writeGlobalObject(in.globalCells[8], in.c.Globals[8].Type, uint64(ref))
					}
					var restore func()
					if kind == "space" || kind == "size" {
						ref := gc.Ref(uint32(readGlobalObject(in.globalCells[8], in.c.Globals[8].Type)))
						view := in.gc.NativeView()
						h := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
						oldView := *view
						old := append([]byte(nil), h...)
						switch kind {
						case "space":
							h[gc.NativeHandleSpaceOffset] = 255
						case "size":
							binary.LittleEndian.PutUint32(h[gc.NativeHandleSizeOffset:], gc.HeaderSize-1)
						}
						restore = func() { *view = oldView; copy(h, old) }
					}
					got, err := in.Invoke(export)
					if restore != nil {
						restore()
					}
					if kind == "valid" || export == "test" && (kind == "wrong" || kind == "null") {
						want := uint64(1)
						if kind != "valid" {
							want = 0
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("result=%v,%v want%d", got, err, want)
						}
					} else {
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
							t.Fatalf("trap=%v want cast failure", err)
						}
						if len(trap.Frames) != 1 || trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != ^uint32(0) {
							t.Errorf("deferred resolver trap falsely claims exact source PC: %+v", trap.Frames)
						}

					}
					for g := 0; g < 8; g++ {
						if got := readGlobalObject(in.globalCells[g], in.c.Globals[g].Type); got != uint64(g+1) {
							t.Errorf("global %d after %s read = %#x; want %d", g, kind, got, g+1)
						}
					}
				})
			}
		}
	}
}
