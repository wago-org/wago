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

func gcLeafTrapPinsModule(leaf string, index byte) []byte {
	globals := make([][]byte, 0, 9)
	for range 8 {
		globals = append(globals, wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}))
	}
	shape := []byte{0x5e, 0x6d, 1}            // final array of mutable eqref
	root := []byte{0x63, 0, 1, 0xd0, 0, 0x0b} // typed nullable global, to select ordinary get/set
	if leaf == "len" || leaf == "len-nullable" {
		shape = []byte{0x5e, 0x7f, 1}
		root = []byte{0x6d, 1, 0xd0, 0x6d, 0x0b}
	}
	if leaf == "struct-set" {
		shape = []byte{0x5f, 1, 0x6d, 1}
	}
	globals = append(globals, root, []byte{0x6d, 1, 0xd0, 0x6d, 0x0b})
	body := []byte{0x03, 0x40}
	for g := byte(0); g < 8; g++ {
		body = append(body, 0x23, g, 0x42, g+1, 0x7c, 0x24, g)
	}
	body = append(body, 0x0b, 0x23, 8)
	switch leaf {
	case "len", "len-nullable":
		cast := byte(22)
		if leaf == "len-nullable" {
			cast = 23
		}
		body = append(body, 0xfb, cast, 0, 0xfb, 15)
	case "get":
		body = append(body, 0x41, index, 0xfb, 11, 0, 0xd1)
	case "struct-set":
		body = append(body, 0xd0, 0x6d, 0xfb, 5, 0, 0, 0x41, 1)
	case "array-set":
		body = append(body, 0x41, index, 0xd0, 0x6d, 0xfb, 14, 0, 0x41, 1)
	}
	body = append(body, 0x0b)
	// Keep native roots required through an observable struct allocation. A
	// distinct cell avoids the older base branch array-allocation cell-cache
	// defect, already fixed in the combined reference-ABI stack.
	allocator := []byte{0xfb, 1, 1, 0x24, 9, 0x41, 0, 0x0b}

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(shape, []byte{0x5f, 1, 0x7e, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(allocator))),
	)
}

func TestGCLeafTrapPreservesGlobalPins(t *testing.T) {
	for _, leaf := range []string{"len", "len-nullable", "get", "struct-set", "array-set"} {
		indices := []byte{0}
		if leaf == "get" || leaf == "array-set" {
			indices = append(indices, 1)
		}
		for _, index := range indices {
			t.Run(fmt.Sprintf("%s/index%d", leaf, index), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLeafTrapPinsModule(leaf, index))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if a := c.GCNativeRootAdmission(); !a.Required || !a.Exact {
					t.Fatalf("native roots=%+v", a)
				}
				// There are no parameters or locals. All three saved values are globals.
				if n := bytes.Count(c.code, []byte{0x41, 0x51, 0x41, 0x52, 0x41, 0x53}); n != 1 {
					t.Fatalf("R9-R11 saves=%d want1", n)
				}

				kinds := []string{"valid", "header"}
				if leaf != "struct-set" {
					kinds = append(kinds, "handle")
				}
				if leaf == "len" || leaf == "len-nullable" {
					kinds = append(kinds, "wrong", "null", "space")
				}
				for ai, artifact := range []*Compiled{c} {
					for _, kind := range kinds {
						t.Run(fmt.Sprintf("artifact%d/%s", ai, kind), func(t *testing.T) {
							in, err := Instantiate(artifact)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							var ref gc.Ref
							if kind != "null" {
								typ := uint32(0)
								if kind == "wrong" {
									typ = 1
								}
								if leaf == "struct-set" || kind == "wrong" {
									ref, err = in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(typ), gc.EmptyRoots{})
								} else {
									ref, err = in.gc.NewArrayDefaultWithRoots(in.requireGCDomainType(typ), 1, gc.EmptyRoots{})
								}
								if err != nil {
									t.Fatal(err)
								}
								writeGlobalObject(in.globalCells[8], in.c.Globals[8].Type, uint64(ref))
							}
							var restore func()
							if kind == "header" || kind == "handle" || kind == "space" {
								v := in.gc.NativeView()
								h := unsafe.Slice((*byte)(offHeapPtr(v.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
								header := unsafe.Slice((*byte)(offHeapPtr(v.Spaces[h[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(h)))), gc.HeaderSize)
								oldHandle := append([]byte(nil), h...)
								oldHeader := append([]byte(nil), header...)
								switch kind {
								case "space":
									h[gc.NativeHandleSpaceOffset] = 255
								case "handle":
									binary.LittleEndian.PutUint32(h[gc.NativeHandleSizeOffset:], gc.PayloadOffset)
								case "header":
									binary.LittleEndian.PutUint32(header[4:], gc.PayloadOffset)
								}
								// Length reads only the header. Shrink its handle below HeaderSize.
								if (leaf == "len" || leaf == "len-nullable") && kind == "handle" {
									binary.LittleEndian.PutUint32(h[gc.NativeHandleSizeOffset:], gc.HeaderSize-1)
								}
								restore = func() { copy(h, oldHandle); copy(header, oldHeader) }
							}
							got, err := in.Invoke("run")
							if restore != nil {
								restore()
							}
							wantTrap := uint32(TrapCastFailure)
							success := kind == "valid" && index == 0 || (leaf == "len" || leaf == "len-nullable") && kind == "header"
							if index == 1 {
								wantTrap = uint32(TrapBuiltin)
							}
							if kind == "null" && leaf == "len-nullable" {
								wantTrap = uint32(TrapNullReference)
							}
							if success {
								if err != nil || len(got) != 1 || got[0] != 1 {
									t.Fatalf("result=%v,%v want1", got, err)
								}
							} else {
								var trap *TrapError
								if !errors.As(err, &trap) || uint32(trap.Code) != wantTrap {
									t.Fatalf("trap=%v want%d", err, wantTrap)
								}
								if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != ^uint32(0) {
									t.Errorf("deferred leaf must retain function and omit ambiguous PC: %+v", trap.Frames)
								}

							}
							for g := 0; g < 8; g++ {
								if got := readGlobalObject(in.globalCells[g], in.c.Globals[g].Type); got != uint64(g+1) {
									t.Errorf("global%d=%#x want%d", g, got, g+1)
								}
							}
						})
					}
				}
			})
		}
	}
}
