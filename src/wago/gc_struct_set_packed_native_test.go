//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The object survives exported calls. Its reference field must survive numeric
// writes and collection; vector prefixes exercise large physical field offsets.
func gcPackedStructSetModule(storage byte, open, child bool, vectors int) []byte {
	fields := append([]byte{}, wasmtest.ULEB(uint32(vectors+2))...)
	for range vectors {
		fields = append(fields, 0x7b, 0)
	}
	fields = append(fields, storage, 1, 0x6d, 1)
	base := append([]byte{0x5f}, fields...)
	if open {
		base = append([]byte{0x50, 0}, base...)
	}
	types := [][]byte{base, {0x5f, 0}}
	actual := byte(0)
	if child {
		extra := append([]byte(nil), fields...)
		// All test field counts have a one-byte encoding except the large-offset
		// fixture, which does not use a child type.
		extra[0]++
		extra = append(extra, 0x7f, 1)
		types = append(types, append([]byte{0x4f, 1, 0, 0x5f}, extra...))
		actual = 2
	}
	ft := uint32(len(types))
	value := wasm.I32
	types = append(types, wasmtest.FuncType([]wasm.ValType{value}, nil), wasmtest.FuncType(nil, []wasm.ValType{value}),
		[]byte{0x60, 2, 0x63, 0, 0x7f, 0}, []byte{0x60, 0, 1, 0x63, 0},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), []byte{0x60, 0, 1, 0x63, 1})
	set := append([]byte{0xfb, 5, 0}, wasmtest.ULEB(uint32(vectors))...)
	get := append([]byte{0xfb, 4, 0}, wasmtest.ULEB(uint32(vectors))...)
	refSet := append([]byte{0xfb, 5, 0}, wasmtest.ULEB(uint32(vectors+1))...)
	init := []byte{0xfb, 1, actual, 0x24, 0, 0x23, 0, 0xfb, 1, 1}
	init = append(init, refSet...)
	init = append(init, 0x23, 0, 0x20, 0)
	init = append(append(init, set...), 0x0b)
	global := append(append([]byte{0x23, 0, 0x20, 0}, set...), 0x0b)
	param := append(append([]byte{0x20, 0, 0x20, 1}, set...), 0x0b)
	local := []byte{1, 1, 0x63, 0, 0x23, 0, 0x21, 1, 0xfb, 1, 1, 0x1a, 0x20, 1, 0x20, 0}
	local = append(append(local, set...), 0x0b)
	// This loop is used only by the i32 fixture. Each store has a runtime value.
	loop := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1, 0x23, 0, 0x20, 1}

	loop = append(loop, set...)
	loop = append(loop, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	raw := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	nullable := append(append([]byte{0xd0, 0, 0x20, 0}, set...), 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(ft), wasmtest.ULEB(ft), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+2), wasmtest.ULEB(ft+3), wasmtest.ULEB(ft+4), wasmtest.ULEB(ft+5), wasmtest.ULEB(ft), wasmtest.ULEB(ft))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("global", 0, 1), wasmtest.ExportEntry("get", 0, 2), wasmtest.ExportEntry("param", 0, 3), wasmtest.ExportEntry("token", 0, 4), wasmtest.ExportEntry("loop", 0, 5), wasmtest.ExportEntry("wrong", 0, 6), wasmtest.ExportEntry("local", 0, 7), wasmtest.ExportEntry("null", 0, 8))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(global), wasmtest.Code(append(append([]byte{0x23, 0}, get...), 0x0b)), wasmtest.Code(param), wasmtest.Code([]byte{0x23, 0, 0x0b}), raw(loop), wasmtest.Code([]byte{0xfb, 1, 1, 0x0b}), raw(local), wasmtest.Code(nullable))),
	)
}

func TestGCPackedStructSetSources(t *testing.T) {
	for _, storage := range []byte{0x78, 0x77} {
		for _, shape := range []struct {
			open, child bool
			vectors     int
		}{{}, {open: true}, {open: true, child: true}, {open: true, child: true, vectors: 20}} {
			t.Run(fmt.Sprintf("%x/open%v/child%v/vectors%d", storage, shape.open, shape.child, shape.vectors), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPackedStructSetModule(storage, shape.open, shape.child, shape.vectors))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 256, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 8192, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}} {
					in, err := Instantiate(c, InstantiateOptions{GC: cfg})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if _, err := in.Invoke("init", 0); err != nil {
						t.Fatal(err)
					}
					parent := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
					child, _, matched, err := in.gc.StructGetTyped(parent, in.requireGCDomainType(0), false, uint32(shape.vectors+1))
					if err != nil || !matched || child.Ref.IsNull() {
						t.Fatalf("reference field fixture: %v,%v,%v", child, matched, err)
					}
					for _, bits := range []uint64{0, 0x80000000, 0xffffffff, 0x7fc12345, 0x8000000000000000, 0x7ff8123456789abc, ^uint64(0)} {
						want := uint64(uint8(bits))
						if storage == 0x77 {
							want = uint64(uint16(bits))
						}
						for _, name := range []string{"global", "local", "param"} {
							args := []uint64{bits}
							if name == "param" {
								token, err := in.Invoke("token")
								if err != nil {
									t.Fatal(err)
								}
								args = []uint64{token[0], bits}
							}
							if _, err := in.Invoke(name, args...); err != nil {
								t.Fatal(err)
							}
							out, err := in.Invoke("get")
							if err != nil || len(out) != 1 || out[0] != want {
								t.Fatalf("%s: stored%x got%v,%v", name, want, out, err)
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
							stored, _, matched, err := in.gc.StructGetTyped(parent, in.requireGCDomainType(0), false, uint32(shape.vectors+1))
							childMatched, childErr := in.gc.RefTest(stored.Ref, gc.RefTestTarget{Kind: gc.RefTestDefined, Type: in.requireGCDomainType(1), Exact: true})
							if err != nil || !matched || stored.Ref != child.Ref || !childMatched || childErr != nil {
								t.Fatalf("numeric store lost reference field: %v,%v,%v child%v,%v", stored, matched, err, childMatched, childErr)
							}
						}
						for _, name := range []string{"null", "param"} {
							args := []uint64{bits}
							if name == "param" {
								args = []uint64{0, bits}
							}
							_, err := in.Invoke(name, args...)
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapNullReference {
								t.Fatalf("%s:null trap:%v", name, err)
							}
						}
						wrong, err := in.Invoke("wrong")
						if err != nil {
							t.Fatal(err)
						}
						if _, err := in.Invoke("param", wrong[0], bits); err == nil {
							t.Fatal("wrong structural token accepted")
						}
					}
				}
			})
		}
	}
}

func TestGCPackedStructSetExtentFallback(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPackedStructSetModule(0x78, true, true, 20))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{DisableCollection: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 17); err != nil {
		t.Fatal(err)
	}
	view := in.gc.NativeView()
	saved := *view
	defer func() { *view = saved }()
	index := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)) >> 1
	handles := append([]byte(nil), unsafe.Slice((*byte)(offHeapPtr(view.Handles)), int(view.HandleCount)*gc.NativeHandleStride)...)
	handle := handles[int(index)*gc.NativeHandleStride:][:gc.NativeHandleStride]
	offset := gc.PayloadOffset + c.GCTypeDescs[0].Fields[20].Offset
	required := offset + 1
	object := make([]byte, required+3)
	for _, mode := range []string{"handle extent", "header extent", "count"} {
		*view = saved
		view.Handles = uintptr(unsafe.Pointer(&handles[0]))
		space := &view.Spaces[handle[gc.NativeHandleSpaceOffset]]
		space.Base, space.Bytes = uintptr(unsafe.Pointer(&object[0])), required
		binary.LittleEndian.PutUint32(handle[gc.NativeHandleOffsetOffset:], 0)
		binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], required)
		binary.LittleEndian.PutUint32(object[4:], required)
		object[offset] = 99
		switch mode {
		case "handle extent":
			binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], required-1)
			binary.LittleEndian.PutUint32(object[4:], required-1)
		case "header extent":
			binary.LittleEndian.PutUint32(object[4:], required-1)
		case "count":
			view.HandleCount = index
		}
		_, err := in.Invoke("global", 23)
		*view = saved
		got, getErr := in.Invoke("get")
		if err != nil || getErr != nil || len(got) != 1 || got[0] != 23 || object[offset] != 99 {
			t.Fatalf("%s: write%v read%v,%v fake%d", mode, err, got, getErr, object[offset])
		}
	}
	runtime.KeepAlive(handles)
	runtime.KeepAlive(object)
}

func TestGCPackedStructSetLargeOffset(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPackedStructSetModule(0x78, true, false, 2060))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 17); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("global", 0xfedcba98); err != nil {
		t.Fatal(err)
	}
	if got, err := in.Invoke("get"); err != nil || len(got) != 1 || got[0] != 0x98 {
		t.Fatalf("large offset: %v,%v", got, err)
	}
}

func TestGCPackedStructSetStaleFallback(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPackedStructSetModule(0x78, true, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 17); err != nil {
		t.Fatal(err)
	}
	stale := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	if _, err := in.Invoke("init", 23); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := in.gc.StructGetTyped(gc.Ref(uint32(stale)), in.requireGCDomainType(0), false, 0); err == nil {
		t.Fatal("fixture retained stale object")
	}
	saved := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, stale)
	_, err = in.Invoke("global", 31)
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, saved)
	if err == nil {
		t.Fatal("stale struct reference accepted")
	}
}

func TestGCPackedStructSetVectorPrefix(t *testing.T) {
	body := []byte{1, 1, 0x63, 0, 0x23, 0, 0x21, 1, 0xfd, 12}
	vector := make([]byte, 16)
	binary.LittleEndian.PutUint64(vector[8:], 17)
	body = append(body, vector...)
	body = append(body, 0x20, 1, 0x20, 0, 0xfb, 5, 0, 0,
		0xfd, 0x1d, 1, // i64x2.extract_lane1 of the prefix
		0x20, 1, 0xfb, 4, 0, 0, 0xad, 0x7c, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x78, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("init", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0xfb, 1, 0, 0x24, 0, 0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	for _, fallback := range []bool{false, true} {
		if fallback && runtime.GOARCH != "arm64" {
			continue // only ARM64 has this helper-fallback selection contract
		}
		view := in.gc.NativeView()
		count := view.HandleCount
		if fallback {
			view.HandleCount = 0
		}
		out, err := in.Invoke("run", 7)
		view.HandleCount = count
		if err != nil || len(out) != 1 || out[0] != 24 {
			t.Fatalf("fallback%v: %v,%v", fallback, out, err)
		}
	}
}

func BenchmarkGCPackedStructSet(b *testing.B) {
	data := gcPackedStructSetModule(0x78, true, false, 0)
	b.Run("compile", func(b *testing.B) {
		b.ReportAllocs()
		size := 0
		for range b.N {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				b.Fatal(err)
			}
			size = c.CodeSize()
			c.Close()
		}
		b.ReportMetric(float64(size), "native-B")
	})
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 0); err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			view := in.gc.NativeView()
			count := view.HandleCount
			defer func() { view.HandleCount = count }()
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				view.HandleCount = count
				if mode == "fallback" || mode == "mixed" && i&1 != 0 {
					view.HandleCount = 0
				}
				got, err := in.Invoke("loop", 1000)
				if err != nil || len(got) != 1 || got[0] != 1000 {
					b.Fatalf("%v,%v", got, err)
				}
			}
			b.StopTimer()
			b.ReportMetric(1000, "guest-ops/op")
		})
	}
}
