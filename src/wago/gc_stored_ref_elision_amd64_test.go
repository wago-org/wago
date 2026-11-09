//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both functions contain proved and checked constructors of the same type.
// A shared allocator stub must distinguish their per-field proof masks.
func storedRefBoundaryModule(open, nullable bool) []byte {
	root := []byte{0x5f, 1, 0x7f, 0}
	if open {
		root = append([]byte{0x50, 0}, root...)
	}
	ref := byte(0x64)
	if nullable {
		ref = 0x63
	}
	types := [][]byte{
		root,
		{0x5f, 1, 0x7e, 0}, // unrelated i64 struct
		{0x5f, 2, ref, 0, 0, ref, 0, 0},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		{0x60, 1, ref, 0, 1, 0x7f}, // host typed argument
		{0x60, 0, 1, 0x64, 0},      // owned root token
		{0x60, 0, 1, 0x64, 1},      // owned unrelated token
	}
	newRoot := []byte{0x41, 0, 0xfb, 0, 0}
	proved := append(append([]byte{}, newRoot...), newRoot...)
	proved = append(proved, 0xfb, 0, 2, 0xd1, 0x1a)
	checked := append(append([]byte{}, proved...), newRoot...)
	checked = append(checked, 0x23, 0, 0xfb, 0, 2, 0xd1, 0x0b)
	mixed := append(append([]byte{}, proved...), 0x23, 0)
	mixed = append(mixed, newRoot...)
	mixed = append(mixed, 0xfb, 0, 2, 0xd1, 0x0b)
	arg := append([]byte{0x20, 0}, newRoot...)
	arg = append(arg, 0xfb, 0, 2, 0xd1, 0x0b)
	names := []string{"checked", "mixed", "arg", "root", "wrong"}
	funcs, exports := make([][]byte, 5), make([][]byte, 5)
	for i, typ := range []byte{3, 3, 4, 5, 6} {
		funcs[i] = []byte{typ}
		exports[i] = wasmtest.ExportEntry(names[i], 0, uint32(i))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec([]byte{ref, 0, 1, 0xfb, 1, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(checked), wasmtest.Code(mixed), wasmtest.Code(arg), wasmtest.Code([]byte{0xfb, 1, 0, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 1, 0x0b}))),
	)
}

func TestStoredRefElisionRetainsDynamicChecks(t *testing.T) {
	for _, open := range []bool{false, true} {
		for _, nullable := range []bool{false, true} {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), storedRefBoundaryModule(open, nullable))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, entry := range []string{"checked", "mixed"} {
				for _, bad := range []string{"subtype", "handle", "free-space", "invalid-space", "i31", "null"} {
					if bad == "null" && nullable {
						continue
					}
					t.Run(fmt.Sprintf("open%v/null%v/%s/%s", open, nullable, entry, bad), func(t *testing.T) {
						in, err := Instantiate(c, InstantiateOptions{})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for range 3 {
							got, err := in.Invoke(entry)
							if err != nil || len(got) != 1 || got[0] != 0 {
								t.Fatalf("valid constructor: %v, %v", got, err)
							}
						}
						original := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
						view := in.gc.NativeView()
						bits := original
						var restore func()
						switch bad {
						case "subtype":
							wrong, err := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(1), gc.EmptyRoots{})
							if err != nil {
								t.Fatal(err)
							}
							bits = uint64(wrong)
						case "handle":
							bits = 0xfffffffe // remains outside the handle table during allocation
						case "i31":
							bits = uint64(gc.I31New(3))
						case "null":
							bits = 0
						case "free-space", "invalid-space":
							handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(uint32(original)>>1)*uintptr(view.HandleStride))), gc.NativeHandleStride)
							saved := handle[gc.NativeHandleSpaceOffset]
							handle[gc.NativeHandleSpaceOffset] = 0
							if bad == "invalid-space" {
								handle[gc.NativeHandleSpaceOffset] = 5
							}
							restore = func() { handle[gc.NativeHandleSpaceOffset] = saved }
						}
						writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, bits)
						_, err = in.Invoke(entry)
						writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original)
						if restore != nil {
							restore()
						}
						field := 1
						if entry == "mixed" {
							field = 0
						}
						want := fmt.Sprintf("gc struct reference type does not match field 2:%d", field)
						if bad == "null" {
							want = fmt.Sprintf("gc struct field 2:%d rejects null", field)
						}
						if bad == "handle" {
							want += "\ngc: invalid object ref"
						}
						if err == nil || err.Error() != "wago: WasmGC struct helper: "+want {
							t.Fatalf("%s: got %v; want helper trap with %q", bad, err, want)
						}
					})
				}
			}
		}
	}
}

func TestStoredRefElisionHostTypedArgumentBoundary(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), storedRefBoundaryModule(true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	root, err := in.Invoke("root")
	if err != nil {
		t.Fatal(err)
	}
	rootToken := root[0] // Invoke reuses its result slice.
	wrong, err := in.Invoke("wrong")
	if err != nil {
		t.Fatal(err)
	}
	wrongToken := wrong[0]
	if got, err := in.Invoke("arg", rootToken); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("owned host ref: %v, %v", got, err)
	}
	for _, tc := range []struct {
		name string
		bits uint64
		want string
	}{
		{"wrong-type-token", wrongToken, "invalid anyref token for argument 0: GC reference token does not match the required structural argument type"},
		{"forged-token", 0xfffffffe, "invalid anyref token for argument 0: invalid or stale GC reference token"},
		{"i31", uint64(gc.I31New(2)), "i31 anyref argument 0 does not match its exact type"},
		{"null", 0, "null anyref for non-null argument 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := in.Invoke("arg", tc.bits)
			if err == nil || got != nil || err.Error() != "arg: "+tc.want {
				t.Fatalf("got %v, %v; want %q", got, err, tc.want)
			}
		})
	}
}
