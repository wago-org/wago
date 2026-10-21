//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNativeNullExternModule() []byte {
	types := [][]byte{
		{0x5f, 1, 0x7f, 1},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	var funcs, bodies, exports [][]byte
	add := func(name string, typ byte, body ...byte) {
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(len(funcs))))
		funcs = append(funcs, []byte{typ})
		bodies = append(bodies, wasmtest.Code(body))
	}
	add("init", 1, 0x41, 1, 0x20, 0, 0xfb, 28, 0x26, 0, 0x41, 2, 0x41, 73, 0xfb, 0, 0, 0x26, 0, 0x0b)
	add("internalize", 2, 0x20, 0, 0xfb, 26, 0xd1, 0x0b)
	add("externalize", 3, 0x20, 0, 0x25, 0, 0xfb, 27, 0xd1, 0x0b)
	add("foreign-roundtrip", 4, 0x20, 0, 0xfb, 26, 0xfb, 27, 0x0b)
	add("data-roundtrip", 3, 0x20, 0, 0x25, 0, 0xfb, 27, 0xfb, 26, 0xfb, 22, 0x6d, 0x20, 0, 0x25, 0, 0xfb, 22, 0x6d, 0xd3, 0x0b)
	add("internalize-global", 5, 0x23, 0, 0xfb, 26, 0xd1, 0x0b)
	add("externalize-global", 5, 0x23, 1, 0xfb, 27, 0xd1, 0x0b)
	add("internalize-static", 5, 0xd0, 0x6f, 0xfb, 26, 0xd1, 0x0b)
	add("externalize-static", 5, 0xd0, 0x6e, 0xfb, 27, 0xd1, 0x0b)
	exports = append(exports, wasmtest.ExportEntry("extern", 3, 0))
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6e, 0, 3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b}, []byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCNativeNullExternIdentityAndClose(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeNullExternModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := in.Invoke("init", 0x7fffffff); err != nil {
			t.Fatal(err)
		}
		state := in.existingGCRefTestTableState()
		if state == nil || state.Conversion == nil {
			t.Fatal("conversion ownership missing")
		}
		for _, name := range []string{"internalize", "externalize", "internalize-global", "externalize-global", "internalize-static", "externalize-static"} {
			args := []uint64(nil)
			if name == "internalize" || name == "externalize" {
				args = []uint64{0}
			}
			got, err := in.Invoke(name, args...)
			if err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("%s(null): %v,%v", name, got, err)
			}
		}
		if state.Conversion.count != 0 {
			t.Fatal("null conversion created an identity")
		}
		foreign, err := in.NewExternRef("foreign")
		if err != nil {
			t.Fatal(err)
		}
		word := ValueExternRef(foreign).Bits()
		global, err := in.ExportedGlobalObject("extern")
		if err != nil {
			t.Fatal(err)
		}
		if err := global.SetValue(ValueExternRef(foreign)); err != nil {
			t.Fatal(err)
		}
		for round := 0; round < 3; round++ {
			if got, err := in.Invoke("internalize", word); err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("foreign predicate: %v,%v", got, err)
			}
			if got, err := in.Invoke("internalize-global"); err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("foreign global: %v,%v", got, err)
			}
			if got, err := in.Invoke("foreign-roundtrip", word); err != nil || len(got) != 1 || got[0] != word {
				t.Fatalf("foreign identity: %x,%v", got, err)
			}
			for index := uint64(1); index < 3; index++ {
				if got, err := in.Invoke("externalize", index); err != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("data predicate%d: %v,%v", index, got, err)
				}
				if got, err := in.Invoke("data-roundtrip", index); err != nil || len(got) != 1 || got[0] != 1 {
					t.Fatalf("data identity%d: %v,%v", index, got, err)
				}
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"internalize-global", "externalize-global", "internalize-static", "externalize-static"} {
			if _, err := in.Invoke(name); err == nil {
				t.Fatalf("%s entered closed instance", name)
			}
		}
	}
}

func TestGCNativeNullExternHighBits(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeNullExternModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	// Bypass host token admission to test both native conversion guards. Restore
	// the valid null cell before a collection or any test failure can unwind.
	for index, name := range []string{"internalize-global", "externalize-global"} {
		ptr := binary.LittleEndian.Uint64(in.globals[index*8:])
		cell := unsafe.Slice((*byte)(offHeapPtr(uintptr(ptr))), 8)
		for _, word := range []uint64{0x100000000, 0x100000001, 0xffffffff00000000, 0xffffffffffffffff} {
			binary.LittleEndian.PutUint64(cell, word)
			got, err := in.Invoke(name)
			binary.LittleEndian.PutUint64(cell, 0)
			if err == nil {
				t.Fatalf("%s(%x) hid invalid identity: %v", name, word, got)
			}
		}
	}
}

// The source is a runtime parameter. The mask selects null on alternate guest
// iterations; ref.is_null keeps extern.convert_any's result observable.
func gcNativeNullExternLoop(op byte) []byte {
	typ, heap := wasm.ExternRef, byte(0x6f)
	if op == 27 {
		typ, heap = wasm.AnyRef, 0x6e
	}
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 3, 0x20, 2, 0x71, 0x04, heap, 0xd0, heap, 0x05, 0x20, 1, 0x0b,
		0xfb, op, 0xd1, 0x1a, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 3, 0x0b}
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(body)))
}

func BenchmarkGCNativeNullExtern(b *testing.B) {
	const iterations = 1024
	for _, op := range []byte{26, 27} {
		for _, path := range []string{"null", "fallback", "mixed"} {
			b.Run(fmt.Sprintf("op%d/%s", op, path), func(b *testing.B) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeNullExternLoop(op))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				word := uint64(0)
				mask := uint64(0)
				if path != "null" {
					foreign, err := in.NewExternRef("benchmark")
					if err != nil {
						b.Fatal(err)
					}
					word = ValueExternRef(foreign).Bits()
				}
				if path == "mixed" {
					mask = 1
				}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					if got, err := in.Invoke("run", iterations, word, mask); err != nil || len(got) != 1 || got[0] != iterations {
						b.Fatalf("run: %v,%v", got, err)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/convert")
			})
		}
	}
}

func BenchmarkGCNativeNullExternCompile(b *testing.B) {
	data := gcNativeNullExternModule()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(c.CodeSize()), "native-B")
		c.Close()
	}
}

func TestGCNativeNullExternLoop(t *testing.T) {
	for _, op := range []byte{26, 27} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeNullExternLoop(op))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := in.NewExternRef("loop")
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range []uint64{0, ValueExternRef(foreign).Bits()} {
			for _, mask := range []uint64{0, 1} {
				if got, err := in.Invoke("run", 8, word, mask); err != nil || len(got) != 1 || got[0] != 8 {
					t.Fatalf("op%d loop: %v,%v", op, got, err)
				}
			}
		}
		in.Close()
		c.Close()
	}
}

func TestGCNativeNullExternTrapOrderAndLivePrefix(t *testing.T) {
	for _, op := range []byte{26, 27} {
		params := make([]wasm.ValType, 18)
		results := make([]wasm.ValType, 17)
		for i := range results {
			params[i], results[i] = wasm.I32, wasm.I32
		}
		params[17] = wasm.ExternRef
		if op == 27 {
			params[17] = wasm.AnyRef
		}
		body := []byte{0x41, 1, 0x20, 0, 0x6e}
		for i := 1; i < 17; i++ {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0x20, 17, 0xfb, op, 0xd1, 0x1a, 0x0b)
		module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := in.NewExternRef("prefix")
		if err != nil {
			t.Fatal(err)
		}
		args := make([]uint64, len(params))
		for i := 1; i < 17; i++ {
			args[i] = uint64(100 + i)
		}
		_, err = in.Invoke("run", args...)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapDivZero {
			t.Fatalf("op%d: %v; want division trap", op, err)
		}
		args[0] = 1
		for _, word := range []uint64{0, ValueExternRef(foreign).Bits()} {
			args[17] = word
			got, err := in.Invoke("run", args...)
			if err != nil || len(got) != len(results) {
				t.Fatalf("op%d prefix: %v,%v", op, got, err)
			}
			for i := range results {
				if got[i] != args[i] {
					t.Fatalf("op%d prefix%d=%d,want%d", op, i, got[i], args[i])
				}
			}
		}
		in.Close()
		c.Close()
	}
}
