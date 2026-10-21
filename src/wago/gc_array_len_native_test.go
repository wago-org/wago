//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// gcGeneralArrayLenModule keeps arrayref abstract at length sites. The array
// reaches those sites through globals, locals, fields, casts and local calls.
func gcGeneralArrayLenModule(storage byte) []byte {
	types := [][]byte{
		{0x5e, storage, 1}, {0x5f, 1, 0x6a, 0},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		{0x60, 1, 0x6a, 1, 0x7f},
		{0x60, 1, 0x6a, 1, 0x6a},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		{0x60, 0, 1, 0x6a},
	}
	local := []byte{1, 1, 0x6a, // one arrayref local
		0x23, 0, 0x21, 0, // retain global in local
		0x41, 1, 0xfb, 7, 0, 0x1a, // allocation can move the retained array
		0x20, 0, 0xfb, 15, 0x0b}
	loop := []byte{1, 2, 0x7f, // loop index and sum
		0x02, 0x40, 0x03, 0x40,
		0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x23, 0, 0xfb, 15, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0,
		0x0b, 0x0b, 0x20, 2, 0x0b}
	body := func(raw []byte) []byte { return append(wasmtest.ULEB(uint32(len(raw))), raw...) }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{2}, []byte{2}, []byte{2}, []byte{4}, []byte{5}, []byte{2}, []byte{2}, []byte{2}, []byte{6}, []byte{7})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6a, 1, 0xd0, 0x6a, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("global", 0, 1),
			wasmtest.ExportEntry("local", 0, 2), wasmtest.ExportEntry("field", 0, 3),
			wasmtest.ExportEntry("param", 0, 4), wasmtest.ExportEntry("nested", 0, 6),
			wasmtest.ExportEntry("cast", 0, 7), wasmtest.ExportEntry("null", 0, 8),
			wasmtest.ExportEntry("loop", 0, 9), wasmtest.ExportEntry("token", 0, 10))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 7, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0xfb, 15, 0x0b}), body(local),
			wasmtest.Code([]byte{0x23, 0, 0xfb, 0, 1, 0xfb, 2, 1, 0, 0xfb, 15, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfb, 15, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x10, 5, 0x10, 4, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0xfb, 22, 0x6a, 0xfb, 15, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x6a, 0xfb, 15, 0x0b}), body(loop),
			wasmtest.Code([]byte{0x23, 0, 0x0b}))),
	)
}

func TestGCGeneralArrayLenSourcesAndCollection(t *testing.T) {
	for _, storage := range []byte{0x78, 0x77, 0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x6d} {
		t.Run(fmt.Sprintf("storage%x", storage), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralArrayLenModule(storage))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, cfg := range []GCConfig{
				{VerifyAfterCollect: true},
				{CollectEveryAlloc: true, StressNurseryBytes: 256, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 8192, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
			} {
				in, err := Instantiate(c, InstantiateOptions{GC: cfg})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for _, length := range []uint64{0, 1, 17, 127} {
					if _, err := in.Invoke("init", length); err != nil {
						t.Fatal(err)
					}
					for round := 0; round < 2; round++ {
						for _, name := range []string{"global", "local", "field", "nested", "cast"} {
							got, err := in.Invoke(name)
							if err != nil || len(got) != 1 || got[0] != length {
								t.Fatalf("%s length %d profile %d: %v, %v", name, length, cfg.Profile, got, err)
							}
						}
						token, err := in.Invoke("token")
						if err != nil {
							t.Fatal(err)
						}
						got, err := in.Invoke("param", token[0])
						if err != nil || len(got) != 1 || got[0] != length {
							t.Fatalf("param: %v, %v", got, err)
						}
						got, err = in.Invoke("loop", 19)
						if err != nil || len(got) != 1 || got[0] != length*19 {
							t.Fatalf("loop: %v, %v", got, err)
						}
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
					}
				}
				for _, name := range []string{"null", "param"} {
					var args []uint64
					if name == "param" {
						args = []uint64{0}
					}
					_, err := in.Invoke(name, args...)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapNullReference {
						t.Fatalf("%s: %v", name, err)
					}
				}
			}
		})
	}
}

func TestGCGeneralArrayLenNativeGuardFallback(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralArrayLenModule(0x7f))
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
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	index := ref >> 1
	fake := append([]byte(nil), unsafe.Slice((*byte)(offHeapPtr(view.Handles)), int(view.HandleCount)*gc.NativeHandleStride)...)
	entry := fake[int(index)*gc.NativeHandleStride:][:gc.NativeHandleStride]
	original := append([]byte(nil), entry...)
	originalRef := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	var falseHeader [gc.HeaderSize]byte
	for _, name := range []string{"count", "free", "unknown space", "offset", "size", "short handle", "space bytes", "short header", "long header", "high bits"} {
		t.Run(name, func(t *testing.T) {
			*view = saved
			copy(entry, original)
			view.Handles = uintptr(unsafe.Pointer(&fake[0]))
			switch name {
			case "count":
				view.HandleCount = index
			case "free":
				entry[gc.NativeHandleSpaceOffset] = gc.NativeSpaceFree
			case "unknown space":
				entry[gc.NativeHandleSpaceOffset] = 255
			case "offset":
				binary.LittleEndian.PutUint32(entry[gc.NativeHandleOffsetOffset:], ^uint32(0))
			case "size":
				binary.LittleEndian.PutUint32(entry[gc.NativeHandleSizeOffset:], ^uint32(0))
			case "short handle":
				binary.LittleEndian.PutUint32(entry[gc.NativeHandleSizeOffset:], gc.HeaderSize-1)
			case "space bytes":
				view.Spaces[entry[gc.NativeHandleSpaceOffset]].Bytes = 0
			case "short header", "long header", "high bits":
				binary.LittleEndian.PutUint32(entry[gc.NativeHandleOffsetOffset:], 0)
				binary.LittleEndian.PutUint32(entry[gc.NativeHandleSizeOffset:], gc.HeaderSize)
				space := &view.Spaces[entry[gc.NativeHandleSpaceOffset]]
				space.Base = uintptr(unsafe.Pointer(&falseHeader[0]))
				space.Bytes = gc.HeaderSize
				size := gc.HeaderSize
				if name == "short header" {
					size--
				} else if name == "long header" {
					size++
				} else {
					// Public boundary checks must reject this non-compact reference.
					writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, originalRef|uint64(1)<<40)
				}
				binary.LittleEndian.PutUint32(falseHeader[4:], size)
				binary.LittleEndian.PutUint32(falseHeader[8:], 777)
			}
			got, err := in.Invoke("global")
			*view = saved
			writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, originalRef)
			goruntime.KeepAlive(fake)
			goruntime.KeepAlive(falseHeader)
			if name == "high bits" {
				if err == nil {
					t.Fatalf("public GC boundary accepted non-compact reference: %v", got)
				}
			} else if err != nil || len(got) != 1 || got[0] != 17 {
				t.Fatalf("fallback: %v, %v", got, err)
			}
		})
	}
}

func TestGCGeneralArrayLenInvalidReferences(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralArrayLenModule(0x7f))
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
	if _, err := in.Invoke("init", 1); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if _, err := in.gc.ArrayLen(gc.Ref(uint32(stale))); err == nil {
		t.Fatal("fixture retained stale object")
	}
	saved := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	for _, ref := range []uint64{stale, 3, 0xfffffffe} {
		writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, ref)
		_, err := in.Invoke("global")
		writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, saved)
		if err == nil {
			t.Fatalf("invalid reference %#x accepted", ref)
		}
	}
}

func BenchmarkGCGeneralArrayLen(b *testing.B) {
	data := gcGeneralArrayLenModule(0x7f)
	b.Run("compile", func(b *testing.B) {
		b.ReportAllocs()
		codeBytes := 0
		for i := 0; i < b.N; i++ {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes = c.CodeSize()
			c.Close()
		}
		b.ReportMetric(float64(codeBytes), "native-B")
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
	if _, err := in.Invoke("init", 17); err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		for _, count := range []uint64{1, 1000} {
			b.Run(fmt.Sprintf("%s/loop%d", mode, count), func(b *testing.B) {
				view := in.gc.NativeView()
				handleCount := view.HandleCount
				defer func() { view.HandleCount = handleCount }()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					view.HandleCount = handleCount
					if mode == "fallback" || mode == "mixed" && i&1 != 0 {
						view.HandleCount = 0
					}
					got, err := in.Invoke("loop", count)
					if err != nil || len(got) != 1 || got[0] != 17*count {
						b.Fatalf("loop: %v, %v", got, err)
					}
				}
			})
		}
	}
}

func TestGCGeneralArrayLenCodec(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralArrayLenModule(0x7f))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	in, err := Instantiate(loaded, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 17); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"global", "local", "field", "nested", "cast"} {
		if got, err := in.Invoke(name); err != nil || len(got) != 1 || got[0] != 17 {
			t.Fatalf("reloaded %s: %v, %v", name, got, err)
		}
	}
}

func TestGCGeneralArrayLenLiveVectorJoin(t *testing.T) {
	vector := []byte{0xfd, 12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	body := append(vector, 0x20, 0, 0x04, 0x40,
		0x23, 0, 0xfb, 15, 0x1a, 0x05, 0x23, 0, 0xfb, 15, 0x1a, 0x0b, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6a, 1, 0xd0, 0x6a, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 7, 0, 0x24, 0, 0x0b}), wasmtest.Code(body))),
	)
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
	if _, err := in.Invoke("init", 17); err != nil {
		t.Fatal(err)
	}
	for _, fallback := range []bool{false, true} {
		view := in.gc.NativeView()
		saved := view.HandleCount
		if fallback {
			view.HandleCount = 0
		}
		for _, branch := range []uint64{0, 1} {
			got, err := in.Invoke("run", branch)
			if err != nil || len(got) != 2 || got[0] != 0x0706050403020100 || got[1] != 0x0f0e0d0c0b0a0908 {
				t.Fatalf("fallback%v branch%d: %x, %v", fallback, branch, got, err)
			}
		}
		view.HandleCount = saved
	}
}
