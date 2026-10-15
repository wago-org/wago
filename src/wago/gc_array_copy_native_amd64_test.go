//go:build linux && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNumericCopyModule(storage byte) []byte {
	setValue := []byte{0x20, 1}
	getValue := []byte{}
	getSub := byte(11)
	switch storage {
	case 0x78, 0x77:
		setValue = append(setValue, 0xa7)
		getSub = 13
		getValue = append(getValue, 0xad)
	case 0x7f:
		setValue = append(setValue, 0xa7)
		getValue = append(getValue, 0xad)
	case 0x7e:
	case 0x7d:
		setValue = append(setValue, 0xa7, 0xbe)
		getValue = append(getValue, 0xbc, 0xad)
	case 0x7c:
		setValue = append(setValue, 0xbf)
		getValue = append(getValue, 0xbd)
	default:
		panic("unsupported test storage")
	}
	set := func(global byte) []byte {
		body := []byte{0x23, global, 0x20, 0}
		body = append(body, setValue...)
		return append(body, 0xfb, 14, 0, 0x0b)
	}
	get := func(global byte) []byte {
		body := []byte{0x23, global, 0x20, 0, 0xfb, getSub, 0}
		body = append(body, getValue...)
		return append(body, 0x0b)
	}
	copyBody := func(dstGlobal, srcGlobal, count byte, dynamic bool) []byte {
		body := []byte{0x23, dstGlobal, 0x20, 0, 0x23, srcGlobal, 0x20, 1}
		if dynamic {
			body = append(body, 0x20, 2)
		} else {
			body = append(body, 0x41, count)
		}
		return append(body, 0xfb, 17, 0, 0, 0x0b)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x4f, 0, 0x5e, storage, 1},
			[]byte{0x60, 1, 0x7f, 0},
			[]byte{0x60, 2, 0x7f, 0x7e, 0},
			[]byte{0x60, 1, 0x7f, 1, 0x7e},
			[]byte{0x60, 2, 0x7f, 0x7f, 0},
			[]byte{0x60, 3, 0x7f, 0x7f, 0x7f, 0},
			[]byte{0x60, 0, 0},
			[]byte{0x60, 1, 0x7f, 0},
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{2}, []byte{3}, []byte{3}, []byte{4}, []byte{4}, []byte{4}, []byte{5}, []byte{4}, []byte{6}, []byte{6}, []byte{7})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("set-dst", 0, 1), wasmtest.ExportEntry("set-src", 0, 2),
			wasmtest.ExportEntry("get-dst", 0, 3), wasmtest.ExportEntry("get-src", 0, 4), wasmtest.ExportEntry("copy-one", 0, 5),
			wasmtest.ExportEntry("copy-zero", 0, 6), wasmtest.ExportEntry("copy-two", 0, 7), wasmtest.ExportEntry("copy-dynamic", 0, 8),
			wasmtest.ExportEntry("copy-overlap", 0, 9), wasmtest.ExportEntry("clear-dst", 0, 10), wasmtest.ExportEntry("clear-src", 0, 11),
			wasmtest.ExportEntry("trap-copy", 0, 12),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 7, 0, 0x24, 0, 0x20, 0, 0xfb, 7, 0, 0x24, 1, 0x0b}),
			wasmtest.Code(set(0)), wasmtest.Code(set(1)), wasmtest.Code(get(0)), wasmtest.Code(get(1)),
			wasmtest.Code(copyBody(0, 1, 1, false)), wasmtest.Code(copyBody(0, 1, 0, false)),
			wasmtest.Code(copyBody(0, 1, 2, false)), wasmtest.Code(copyBody(0, 1, 0, true)),
			wasmtest.Code(copyBody(0, 0, 2, false)),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd0, 0, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0x41, 1, 0x20, 0, 0x6e, 0x23, 0, 0x41, 0, 0x23, 1, 0x41, 0, 0x41, 1, 0xfb, 17, 0, 0, 0x1a, 0x0b}),
		)),
	)
}

func gcCopyExpected(storage byte, bits uint64) uint64 {
	switch storage {
	case 0x78:
		return bits & 0xff
	case 0x77:
		return bits & 0xffff
	case 0x7f, 0x7d:
		return bits & 0xffffffff
	default:
		return bits
	}
}

func TestGCNativeNumericArrayCopy(t *testing.T) {
	storages := []struct {
		name    string
		storage byte
		bits    uint64
	}{
		{"i8", 0x78, 0xab}, {"i16", 0x77, 0xbeef}, {"i32", 0x7f, 0xdeadbeef},
		{"i64", 0x7e, 0x0123456789abcdef}, {"f32", 0x7d, 0x7fc12345}, {"f64", 0x7c, 0x7ff8000000001234},
	}
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		for _, tc := range storages {
			t.Run(fmt.Sprintf("profile%d/%s", profile, tc.name), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericCopyModule(tc.storage))
				if err != nil {
					t.Fatal(err)
				}
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				defer c.Close()
				expectTrap := func(export string, want TrapCode, args ...uint64) {
					t.Helper()
					_, err := in.Invoke(export, args...)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != want {
						t.Fatalf("%s%v = %v; want %v", export, args, err, want)
					}
				}
				if _, err := in.Invoke("init", 8); err != nil {
					t.Fatal(err)
				}
				want := gcCopyExpected(tc.storage, tc.bits)
				if _, err := in.Invoke("set-src", 3, tc.bits); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("copy-one", 1, 3); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("get-dst", 1); err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("one-element copy = %v, %v; want %#x", got, err, want)
				}
				if _, err := in.Invoke("copy-one", uint64(0xdeadbeef00000002), uint64(0xcafebabe00000003)); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("get-dst", 2); err != nil || got[0] != want {
					t.Fatalf("dirty-index copy = %v, %v; want %#x", got, err, want)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("copy-one", 6, 3); err != nil {
					t.Fatalf("copy after collection: %v", err)
				}
				if got, err := in.Invoke("get-dst", 6); err != nil || got[0] != want {
					t.Fatalf("copy after collection = %v, %v; want %#x", got, err, want)
				}
				if _, err := in.Invoke("copy-zero", 8, 8); err != nil {
					t.Fatalf("zero copy at end: %v", err)
				}
				expectTrap("copy-zero", TrapBuiltin, 9, 8)
				expectTrap("copy-one", TrapBuiltin, 8, 0)
				expectTrap("copy-one", TrapBuiltin, 0, 8)
				bits2 := tc.bits ^ 0x55aa55aa55aa55aa
				want2 := gcCopyExpected(tc.storage, bits2)
				if _, err := in.Invoke("set-dst", 4, tc.bits); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("set-dst", 5, bits2); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("copy-overlap", 5, 4); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("get-dst", 5); err != nil || got[0] != want {
					t.Fatalf("overlap copy = %v, %v; want %#x", got, err, want)
				}
				if got, err := in.Invoke("get-dst", 6); err != nil || got[0] != want2 {
					t.Fatalf("forward overlap tail = %v, %v; want %#x", got, err, want2)
				}
				if _, err := in.Invoke("copy-overlap", 4, 5); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("get-dst", 4); err != nil || got[0] != want {
					t.Fatalf("backward overlap head = %v, %v; want %#x", got, err, want)
				}
				if got, err := in.Invoke("get-dst", 5); err != nil || got[0] != want2 {
					t.Fatalf("backward overlap tail = %v, %v; want %#x", got, err, want2)
				}
				if _, err := in.Invoke("set-src", 2, bits2); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("copy-two", 0, 2); err != nil {
					t.Fatalf("two-element copy: %v", err)
				}
				if got, err := in.Invoke("get-dst", 0); err != nil || got[0] != want2 {
					t.Fatalf("two-element head = %v, %v; want %#x", got, err, want2)
				}
				if got, err := in.Invoke("get-dst", 1); err != nil || got[0] != want {
					t.Fatalf("two-element tail = %v, %v; want %#x", got, err, want)
				}
				expectTrap("copy-two", TrapBuiltin, 7, 0)
				expectTrap("copy-two", TrapBuiltin, 0, 7)
				if _, err := in.Invoke("copy-dynamic", 0, 3, 1); err != nil {
					t.Fatalf("dynamic helper control: %v", err)
				}
				if _, err := in.Invoke("clear-src"); err != nil {
					t.Fatal(err)
				}
				expectTrap("copy-zero", TrapNullReference, 0, 0)
				expectTrap("copy-one", TrapNullReference, 0, 0)
				expectTrap("copy-two", TrapNullReference, 0, 0)
				expectTrap("copy-one", TrapNullReference, 9, 0)
				for _, trapCase := range []struct {
					divisor uint64
					want    TrapCode
				}{{0, TrapDivZero}, {1, TrapNullReference}} {
					_, err := in.Invoke("trap-copy", trapCase.divisor)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != trapCase.want {
						t.Fatalf("trap-copy(%d) = %v; want %v", trapCase.divisor, err, trapCase.want)
					}
				}
				if _, err := in.Invoke("init", 8); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("clear-dst"); err != nil {
					t.Fatal(err)
				}
				expectTrap("copy-zero", TrapNullReference, 0, 0)
				expectTrap("copy-one", TrapNullReference, 0, 0)
				expectTrap("copy-two", TrapNullReference, 0, 0)
			})
		}
	}
}

func TestGCNativeArrayCopyTwoShortObjectSize(t *testing.T) {
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		for _, target := range []int{0, 1} {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericCopyModule(0x7f))
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := in.Invoke("init", 8); err != nil {
				t.Fatal(err)
			}
			for i, value := range []uint64{17, 19} {
				if _, err := in.Invoke("set-src", uint64(i), value); err != nil {
					t.Fatal(err)
				}
			}
			before0, err := in.Invoke("get-dst", 0)
			if err != nil {
				t.Fatal(err)
			}
			before1, err := in.Invoke("get-dst", 1)
			if err != nil {
				t.Fatal(err)
			}
			view := in.gc.NativeView()
			ref := uint32(readGlobalObject(in.globalCells[target], in.c.Globals[target].Type))
			handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
			offset := binary.LittleEndian.Uint32(handle[gc.NativeHandleOffsetOffset:])
			space := handle[gc.NativeHandleSpaceOffset]
			header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[space].Base+uintptr(offset))), gc.PayloadOffset)
			size := binary.LittleEndian.Uint32(header[4:])
			binary.LittleEndian.PutUint32(header[4:], gc.PayloadOffset+4)
			_, copyErr := in.Invoke("copy-two", 0, 0)
			binary.LittleEndian.PutUint32(header[4:], size)
			after0, readErr0 := in.Invoke("get-dst", 0)
			after1, readErr1 := in.Invoke("get-dst", 1)
			in.Close()
			c.Close()
			var trap *TrapError
			if !errors.As(copyErr, &trap) || trap.Code != TrapCastFailure || readErr0 != nil || readErr1 != nil || before0[0] != after0[0] || before1[0] != after1[0] {
				t.Fatalf("profile%v target%d: copy=%v read=%v/%v destination=%x,%x->%x,%x", profile, target, copyErr, readErr0, readErr1, before0[0], before1[0], after0[0], after1[0])
			}
		}
	}
}

func TestGCNativeArrayCopyShortHandleExtent(t *testing.T) {
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		for _, target := range []int{0, 1} {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericCopyModule(0x7f))
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := in.Invoke("init", 8); err != nil {
				t.Fatal(err)
			}
			if _, err := in.Invoke("set-src", 1, 17); err != nil {
				t.Fatal(err)
			}
			before, err := in.Invoke("get-dst", 1)
			if err != nil {
				t.Fatal(err)
			}
			view := in.gc.NativeView()
			ref := uint32(readGlobalObject(in.globalCells[target], in.c.Globals[target].Type))
			handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
			size := binary.LittleEndian.Uint32(handle[gc.NativeHandleSizeOffset:])
			binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], gc.HeaderSize)
			_, copyErr := in.Invoke("copy-one", 1, 1)
			binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], size)
			after, readErr := in.Invoke("get-dst", 1)
			in.Close()
			c.Close()
			var trap *TrapError
			if !errors.As(copyErr, &trap) || trap.Code != TrapCastFailure || readErr != nil || before[0] != after[0] {
				t.Fatalf("profile%v target%d: copy=%v read=%v destination=%x->%x", profile, target, copyErr, readErr, before[0], after[0])
			}
		}
	}
}
