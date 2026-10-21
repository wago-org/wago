//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcGeneralScalarArraySetModule(storage byte, open, child bool) []byte {
	array := []byte{0x5e, storage, 1}
	if open {
		array = append([]byte{0x50, 0}, array...)
	}
	types := [][]byte{array, {0x5f, 0}}
	actual := byte(0)
	if child {
		types = append(types, []byte{0x4f, 1, 0, 0x5e, storage, 1})
		actual = 2
	}
	holder := byte(len(types))
	types = append(types, []byte{0x5f, 1, 0x63, 0, 0})
	ft := uint32(len(types))
	value := map[byte]wasm.ValType{0x7f: wasm.I32, 0x7e: wasm.I64, 0x7d: wasm.F32, 0x7c: wasm.F64}[storage]
	types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32, value}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, value}, []wasm.ValType{value}), []byte{0x60, 3, 0x63, 0, 0x7f, storage, 1, storage}, []byte{0x60, 0, 1, 0x63, 0}, []byte{0x60, 0, 1, 0x63, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{value}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, value}, []wasm.ValType{wasm.I32}))
	set := []byte{0xfb, 14, 0}
	get := []byte{0xfb, 11, 0}
	read := []byte{0x23, 0, 0x20, 0}
	read = append(read, get...)
	finish := func(body []byte) []byte { return append(append(body, read...), 0x0b) }
	global := finish(append([]byte{0x23, 0, 0x20, 0, 0x20, 1}, set...))
	local := []byte{1, 1, 0x63, 0, 0x23, 0, 0x21, 2, 0xfb, 1, 1, 0x1a, 0x20, 2, 0x20, 0, 0x20, 1}
	local = finish(append(local, set...))
	field := []byte{0x23, 0, 0xfb, 0, holder, 0xfb, 2, holder, 0, 0x20, 0, 0x20, 1}
	field = finish(append(field, set...))
	cast := finish(append([]byte{0x23, 0, 0xfb, 22, 0, 0x20, 0, 0x20, 1}, set...))
	param := append([]byte{0x20, 0, 0x20, 1, 0x20, 2}, set...)
	param = append(param, 0x20, 0, 0x20, 1)
	param = append(param, get...)
	param = append(param, 0x0b)
	null := finish(append([]byte{0xd0, 0, 0x20, 0, 0x20, 1}, set...))
	loop := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 0, 0x4f, 0x0d, 1, 0x23, 0, 0x20, 1, 0x20, 2}
	loop = append(loop, set...)
	loop = append(loop, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 3, 0x0b)
	raw := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(ft), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+2), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+3), wasmtest.ULEB(ft+4), wasmtest.ULEB(ft+5), wasmtest.ULEB(ft+6))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("global", 0, 1), wasmtest.ExportEntry("local", 0, 2), wasmtest.ExportEntry("field", 0, 3), wasmtest.ExportEntry("param", 0, 4), wasmtest.ExportEntry("nested", 0, 5), wasmtest.ExportEntry("cast", 0, 6), wasmtest.ExportEntry("null", 0, 7), wasmtest.ExportEntry("token", 0, 8), wasmtest.ExportEntry("wrong", 0, 9), wasmtest.ExportEntry("read", 0, 10), wasmtest.ExportEntry("loop", 0, 11))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0xfb, 6, actual, 0x24, 0, 0x0b}), wasmtest.Code(global), raw(local), wasmtest.Code(field), wasmtest.Code(param), wasmtest.Code([]byte{0x23, 0, 0x20, 0, 0x20, 1, 0x10, 4, 0x0b}), wasmtest.Code(cast), wasmtest.Code(null), wasmtest.Code([]byte{0x23, 0, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 1, 0x0b}), wasmtest.Code(append(read, 0x0b)), raw(loop))))
}

func TestGCGeneralScalarArraySetSources(t *testing.T) {
	for _, storage := range []byte{0x7f, 0x7e, 0x7d, 0x7c} {
		for _, shape := range []struct{ open, child bool }{{}, {open: true}, {open: true, child: true}} {
			t.Run(fmt.Sprintf("%x/open%v/child%v", storage, shape.open, shape.child), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralScalarArraySetModule(storage, shape.open, shape.child))
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
					if _, err := in.Invoke("init", 17, 0); err != nil {
						t.Fatal(err)
					}
					for _, bits := range []uint64{0, 0x80000000, 0xffffffff, 0x7fc12345, 0x8000000000000000, 0x7ff8123456789abc} {
						want := bits
						if storage == 0x7f || storage == 0x7d {
							want = uint64(uint32(bits))
						}
						for round := 0; round < 2; round++ {
							for _, name := range []string{"global", "local", "field", "nested", "cast"} {
								out, err := in.Invoke(name, 16, bits)
								if err != nil || len(out) != 1 || out[0] != want {
									t.Fatalf("%s bits%x:%x,%v", name, bits, out, err)
								}
							}
							token, err := in.Invoke("token")
							if err != nil {
								t.Fatal(err)
							}
							out, err := in.Invoke("param", token[0], 16, bits)
							if err != nil || len(out) != 1 || out[0] != want {
								t.Fatalf("param:%x,%v", out, err)
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			})
		}
	}
}

func TestGCGeneralScalarArraySetTraps(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralScalarArraySetModule(0x7f, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, length := range []uint64{0, 1, 17} {
		if _, err := in.Invoke("init", length, 23); err != nil {
			t.Fatal(err)
		}
		for _, index := range []uint64{length, 0xffffffff} {
			_, err := in.Invoke("global", index, 99)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
				t.Fatalf("length%d index%d:%v", length, index, err)
			}
			_, err = in.Invoke("null", index, 99)
			trap = nil
			if !errors.As(err, &trap) || trap.Code != TrapNullReference {
				t.Fatalf("null index%d:%v", index, err)
			}
		}
		if length > 0 {
			out, err := in.Invoke("read", 0)
			if err != nil || out[0] != 23 {
				t.Fatalf("trap changed live element:%x,%v", out, err)
			}
		}
	}
	token, err := in.Invoke("wrong")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("param", token[0], 0, 99); err == nil {
		t.Fatal("wrong structural token accepted")
	}
}
