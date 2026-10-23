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

var gcSpaceByteCases = []string{"inline-struct", "shared-open-struct", "defined-cast", "defined-test", "cast-array-length", "array-reference-get", "cast-struct-get"}

// Each module executes one checked read per guest-loop iteration. A mutable
// global supplies the object, so constructor forwarding cannot remove the read.
func gcSpaceByteModule(name string) ([]byte, uint32) {
	typ := []byte{0x5f, 1, 0x7f, 1}
	global := []byte{0x63, 0, 1, 0x41, 42, 0xfb, 0, 0, 0x0b}
	op := []byte{0xfb, 2, 0, 0}
	want := uint32(42)
	switch name {
	case "shared-open-struct":
		typ = append([]byte{0x50, 0}, typ...)
	case "defined-cast", "defined-test", "cast-struct-get":
		global = append([]byte{0x6d, 1}, global[3:]...)
		switch name {
		case "defined-cast":
			op, want = []byte{0xfb, 22, 0, 0xd1, 0x45}, 1
		case "defined-test":
			op, want = []byte{0xfb, 20, 0}, 1
		case "cast-struct-get":
			op = []byte{0xfb, 22, 0, 0xfb, 2, 0, 0}
		}
	case "cast-array-length", "array-reference-get":
		typ = []byte{0x5e, 0x6d, 1}
		global = []byte{0x63, 0, 1, 0xd0, 0x6d, 0x41, 4, 0xfb, 6, 0, 0x0b}
		op, want = []byte{0x41, 0, 0xfb, 11, 0, 0xd1}, 1
		if name == "cast-array-length" {
			global = append([]byte{0x6d, 1}, global[3:]...)
			op, want = []byte{0xfb, 22, 0, 0xfb, 15}, 4
		}
	}
	body := []byte{1, 2, 0x7f, 0x02, 0x40, 0x03, 0x40,
		0x20, 1, 0x20, 0, 0x4f, 0x0d, 1, 0x20, 2, 0x23, 0}
	body = append(body, op...)
	body = append(body, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0,
		0x0b, 0x0b, 0x20, 2, 0x0b)
	// A real allocating function admits exact native roots for this module.
	// Without it, the legacy boundary collector overrides the requested profile.
	prefix := 3
	if global[0] == 0x6d {
		prefix = 2
	}
	init := append([]byte(nil), global[prefix:len(global)-1]...)
	init = append(init, 0x24, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(typ, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("init", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code(init))),
	), want
}

func TestGCResolverSpaceByteAcrossSpaces(t *testing.T) {
	for _, name := range gcSpaceByteCases {
		t.Run(name, func(t *testing.T) {
			module, want := gcSpaceByteModule(name)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !c.GCNativeRootAdmission().Exact {
				t.Fatal("fixture requires exact native roots")
			}
			var artifact bytes.Buffer
			if _, err := c.WriteTo(&artifact); err != nil {
				t.Fatal(err)
			}
			var loaded Compiled
			if _, err := loaded.ReadFrom(&artifact); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			for _, saved := range []bool{false, true} {
				compiled := c
				if saved {
					compiled = &loaded
				}
				for _, space := range []byte{gc.NativeSpaceNursery, gc.NativeSpaceOld, gc.NativeSpaceLarge, gc.NativeSpaceTiny} {
					t.Run(fmt.Sprintf("saved=%v/space=%d", saved, space), func(t *testing.T) {
						cfg := GCConfig{VerifyAfterCollect: true}
						if space == gc.NativeSpaceLarge {
							cfg.LargeObjectBytes = 16
						}
						if space == gc.NativeSpaceTiny {
							cfg = GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
						}
						in, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if in.c.genericGCFrameRoots() == nil || !in.gc.CollectsOnAllocation() {
							t.Fatal("fixture must use the requested collector, without boundary collection")
						}
						ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
						if space == gc.NativeSpaceOld {
							if err := in.gc.ForcePromote(ref); err != nil {
								t.Fatal(err)
							}
						}
						view := in.gc.NativeView()
						handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(uint32(ref)>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
						if got := handle[gc.NativeHandleSpaceOffset]; got != space {
							t.Fatalf("fixture space = %d, want %d", got, space)
						}
						for round := 0; round < 2; round++ {
							got, err := in.Invoke("run", 17)
							if err != nil || len(got) != 1 || got[0] != uint64(17*want) {
								t.Fatalf("read = %v, %v; want %d", got, err, 17*want)
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
						}
					})
				}
			}
		})
	}
}

func TestGCResolverSpaceByteRejectsInvalidEntries(t *testing.T) {
	for _, name := range gcSpaceByteCases {
		t.Run(name, func(t *testing.T) {
			module, want := gcSpaceByteModule(name)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.c.genericGCFrameRoots() == nil || !in.gc.CollectsOnAllocation() {
				t.Fatal("fixture must reach native guards without boundary collection")
			}
			ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
			view := in.gc.NativeView()
			handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
			saved := append([]byte(nil), handle...)
			defer copy(handle, saved)
			check := func(label string, bad bool) {
				t.Helper()
				got, err := in.Invoke("run", 1)
				if bad {
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
						t.Fatalf("%s = %v, %v; want native cast trap", label, got, err)
					}
				} else if err != nil || len(got) != 1 || got[0] != uint64(want) {
					t.Fatalf("%s = %v, %v; want %d", label, got, err, want)
				}
			}
			// Adjacent metadata bytes must not enter the space index.
			handle[12], handle[13], handle[15] = 0xff, 0x80, 0xff
			check("nonzero class and remembered bytes", false)
			for space := 0; space <= 255; space++ {
				if space >= 1 && space <= 4 {
					continue
				}
				handle[gc.NativeHandleSpaceOffset] = byte(space)
				check(fmt.Sprintf("invalid space %d", space), true)
			}
			for _, tc := range []struct {
				name   string
				offset int
				value  uint32
			}{
				{"short handle", gc.NativeHandleSizeOffset, gc.HeaderSize - 1},
				{"large size", gc.NativeHandleSizeOffset, ^uint32(0)},
				{"large offset", gc.NativeHandleOffsetOffset, ^uint32(0)},
			} {
				copy(handle, saved)
				binary.LittleEndian.PutUint32(handle[tc.offset:], tc.value)
				check(tc.name, true)
			}
			copy(handle, saved)
			check("restored entry", false)
		})
	}
}

func BenchmarkGCResolverSpaceByte(b *testing.B) {
	for _, name := range gcSpaceByteCases {
		b.Run(name, func(b *testing.B) {
			module, want := gcSpaceByteModule(name)
			benchmarkGCInstructionLoop(b, module, want)
		})
	}
}
