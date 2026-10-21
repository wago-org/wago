//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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

func BenchmarkGCGeneralArrayLen(b *testing.B) {
	data := gcGeneralArrayLenModule(0x7f)
	b.Run("compile", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				b.Fatal(err)
			}
			c.Close()
		}
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
	for _, count := range []uint64{1, 1000} {
		b.Run(fmt.Sprintf("loop%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("loop", count)
				if err != nil || len(got) != 1 || got[0] != 17*count {
					b.Fatalf("loop: %v, %v", got, err)
				}
			}
		})
	}
}
