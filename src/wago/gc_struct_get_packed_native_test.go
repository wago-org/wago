//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcPackedStructGetModule(storage byte, signed, mutable, open, child bool, vectors int) []byte {
	fields := make([]byte, 0, vectors*2+5)
	fields = append(fields, wasmtest.ULEB(uint32(vectors+2))...)
	for i := 0; i < vectors; i++ {
		fields = append(fields, 0x7b, 0)
	}
	mut := byte(0)
	if mutable {
		mut = 1
	}
	fields = append(fields, storage, mut, 0x7e, 0)
	base := append([]byte{0x5f}, fields...)
	if open {
		base = append([]byte{0x50, 0}, base...)
	}
	types := [][]byte{base, {0x5f, 0}}
	actual := byte(0)
	if child {
		extra := append([]byte(nil), fields...)
		extra[0]++
		extra = append(extra, 0x7f, 0)
		types = append(types, append([]byte{0x4f, 1, 0, 0x5f}, extra...))
		actual = 2
	}
	ft := uint32(len(types))
	value := wasm.I32
	types = append(types,
		wasmtest.FuncType([]wasm.ValType{value}, nil),
		wasmtest.FuncType(nil, []wasm.ValType{value}),
		[]byte{0x60, 1, 0x63, 0, 1, 0x7f},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		[]byte{0x60, 0, 1, 0x63, 0}, []byte{0x60, 0, 1, 0x63, 1})
	op := byte(4)
	if signed {
		op = 3
	}
	get := append([]byte{0xfb, op, 0}, wasmtest.ULEB(uint32(vectors))...)
	init := []byte{}
	for i := 0; i < vectors; i++ {
		init = append(init, 0xfd, 12)
		init = append(init, make([]byte, 16)...)
	}
	init = append(init, 0x20, 0, 0x42, 7)
	if child {
		init = append(init, 0x41, 9)
	}
	init = append(init, 0xfb, 0, actual, 0x24, 0, 0x0b)
	global := append(append([]byte{0x23, 0}, get...), 0x0b)
	param := append(append([]byte{0x20, 0}, get...), 0x0b)
	local := []byte{1, 1, 0x63, 0, 0x23, 0, 0x21, 0, 0xfb, 1, 1, 0x1a, 0x20, 0}
	local = append(append(local, get...), 0x0b)
	loop := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1, 0x23, 0}
	loop = append(loop, get...)
	loop = append(loop, 0x1a, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	raw := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	nullable := append(append([]byte{0xd0, 0}, get...), 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(ft), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+2), wasmtest.ULEB(ft+3), wasmtest.ULEB(ft+4), wasmtest.ULEB(ft+5), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("global", 0, 1), wasmtest.ExportEntry("param", 0, 2), wasmtest.ExportEntry("loop", 0, 3), wasmtest.ExportEntry("token", 0, 4), wasmtest.ExportEntry("wrong", 0, 5), wasmtest.ExportEntry("local", 0, 6), wasmtest.ExportEntry("null", 0, 7))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(global), wasmtest.Code(param), raw(loop), wasmtest.Code([]byte{0x23, 0, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 1, 0x0b}), raw(local), wasmtest.Code(nullable))),
	)
}

func TestGCPackedStructGetSources(t *testing.T) {
	for _, storage := range []byte{0x78, 0x77} {
		for _, signed := range []bool{false, true} {
			for _, mutable := range []bool{false, true} {
				for _, shape := range []struct {
					open, child bool
					vectors     int
				}{{}, {open: true}, {open: true, child: true}, {open: true, child: true, vectors: 20}} {
					t.Run(fmt.Sprintf("%x/signed%v/mutable%v/open%v/child%v/vectors%d", storage, signed, mutable, shape.open, shape.child, shape.vectors), func(t *testing.T) {
						c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPackedStructGetModule(storage, signed, mutable, shape.open, shape.child, shape.vectors))
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
							for _, bits := range []uint32{0, 1, 0x7f, 0x80, 0xff, 0x7fff, 0x8000, 0xffff, 0x12345678, 0xffffffff} {
								want := uint32(uint8(bits))
								if storage == 0x77 {
									want = uint32(uint16(bits))
								}
								if signed {
									if storage == 0x78 {
										want = uint32(int32(int8(bits)))
									} else {
										want = uint32(int32(int16(bits)))
									}
								}
								if _, err := in.Invoke("init", uint64(bits)); err != nil {
									t.Fatal(err)
								}
								for round := 0; round < 2; round++ {
									for _, name := range []string{"global", "local", "param"} {
										var args []uint64
										if name == "param" {
											token, err := in.Invoke("token")
											if err != nil {
												t.Fatal(err)
											}
											args = token
										}
										got, err := in.Invoke(name, args...)
										if err != nil || len(got) != 1 || got[0] != uint64(want) {
											t.Fatalf("%s(%x):%x,%v,want%x", name, bits, got, err, want)
										}
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
									t.Fatalf("%s:null trap%v", name, err)
								}
							}
							token, err := in.Invoke("wrong")
							if err != nil {
								t.Fatal(err)
							}
							if _, err := in.Invoke("param", token[0]); err == nil {
								t.Fatal("wrong structural token accepted")
							}
						}
					})
				}
			}
		}
	}
}

func TestGCPackedStructGetLargeOffset(t *testing.T) {
	for _, storage := range []byte{0x78, 0x77} {
		const fields = 5000
		structure := append([]byte{0x5f}, wasmtest.ULEB(fields)...)
		for range fields {
			structure = append(structure, storage, 1)
		}
		index := wasmtest.ULEB(fields - 1)
		body := []byte{1, 1, 0x63, 0, 0xfb, 1, 0, 0x22, 0, 0x41, 0x7f, 0xfb, 5, 0}
		body = append(body, index...)
		body = append(body, 0x20, 0, 0xfb, 4, 0)
		body = append(body, index...)
		body = append(body, 0x0b)
		module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(structure, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
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
		want := uint64(255)
		if storage == 0x77 {
			want = 65535
		}
		if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("large %x:%v,%v", storage, got, err)
		}
	}
}

func BenchmarkGCPackedStructGet(b *testing.B) {
	data := gcPackedStructGetModule(0x78, true, true, true, false, 0)
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
	if _, err := in.Invoke("init", 255); err != nil {
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
