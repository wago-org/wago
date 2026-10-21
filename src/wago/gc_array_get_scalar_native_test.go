//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Exported calls retain the array, so constructor forwarding cannot remove
// reads. Open arrays and collection exercise cases outside the nursery path.
func gcGeneralScalarArrayGetModule(storage byte, mutable, open, child bool) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	array := []byte{0x5e, storage, mut}
	if open {
		array = append([]byte{0x50, 0}, array...)
	}
	types := [][]byte{array, {0x5f, 0}}
	actual := byte(0)
	if child {
		types = append(types, []byte{0x4f, 1, 0, 0x5e, storage, mut})
		actual = 2
	}
	holder := byte(len(types))
	types = append(types, []byte{0x5f, 1, 0x63, 0, 0})
	ft := uint32(len(types))
	value := map[byte]wasm.ValType{0x7f: wasm.I32, 0x7e: wasm.I64, 0x7d: wasm.F32, 0x7c: wasm.F64}[storage]
	types = append(types,
		wasmtest.FuncType([]wasm.ValType{wasm.I32, value}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{value}),
		[]byte{0x60, 2, 0x63, 0, 0x7f, 1, storage},
		[]byte{0x60, 0, 1, 0x63, 0},
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		[]byte{0x60, 1, 0x63, 0, 1, 0x63, 0},
		[]byte{0x60, 0, 1, 0x63, 1})
	get := []byte{0xfb, 11, 0}
	global := append(append([]byte{0x23, 0, 0x20, 0}, get...), 0x0b)
	param := append(append([]byte{0x20, 0, 0x20, 1}, get...), 0x0b)
	local := []byte{1, 1, 0x63, 0, 0x23, 0, 0x21, 1, 0xfb, 1, 1, 0x1a, 0x20, 1, 0x20, 0}
	local = append(append(local, get...), 0x0b)
	field := []byte{0x23, 0, 0xfb, 0, holder, 0xfb, 2, holder, 0, 0x20, 0}
	field = append(append(field, get...), 0x0b)
	cast := append(append([]byte{0x23, 0, 0xfb, 22, 0, 0x20, 0}, get...), 0x0b)
	null := append(append([]byte{0xd0, 0, 0x20, 0}, get...), 0x0b)
	loop := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x4f, 0x0d, 1, 0x23, 0, 0x20, 1}
	loop = append(loop, get...)
	loop = append(loop, 0x1a, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b)
	raw := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(ft), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+2), wasmtest.ULEB(ft+5), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+1), wasmtest.ULEB(ft+4), wasmtest.ULEB(ft+3), wasmtest.ULEB(ft+6))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("global", 0, 1), wasmtest.ExportEntry("local", 0, 2), wasmtest.ExportEntry("field", 0, 3), wasmtest.ExportEntry("param", 0, 4), wasmtest.ExportEntry("nested", 0, 6), wasmtest.ExportEntry("cast", 0, 7), wasmtest.ExportEntry("null", 0, 8), wasmtest.ExportEntry("loop", 0, 9), wasmtest.ExportEntry("token", 0, 10), wasmtest.ExportEntry("wrong", 0, 11))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0xfb, 6, actual, 0x24, 0, 0x0b}), wasmtest.Code(global), raw(local), wasmtest.Code(field), wasmtest.Code(param), wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0x10, 5, 0x20, 0, 0x10, 4, 0x0b}), wasmtest.Code(cast), wasmtest.Code(null), raw(loop), wasmtest.Code([]byte{0x23, 0, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 1, 0x0b}))),
	)
}

func TestGCGeneralScalarArrayGetSources(t *testing.T) {
	for _, storage := range []byte{0x7f, 0x7e, 0x7d, 0x7c} {
		for _, mutable := range []bool{false, true} {
			for _, shape := range []struct{ open, child bool }{{}, {open: true}, {open: true, child: true}} {
				t.Run(fmt.Sprintf("%x/mutable%v/open%v/child%v", storage, mutable, shape.open, shape.child), func(t *testing.T) {
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralScalarArrayGetModule(storage, mutable, shape.open, shape.child))
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
						for _, length := range []uint64{1, 17, 127} {
							for _, bits := range []uint64{0, 0x80000000, 0xffffffff, 0x7fc12345, 0x8000000000000000, 0x7ff8123456789abc} {
								want := bits
								if storage == 0x7f || storage == 0x7d {
									want = uint64(uint32(bits))
								}
								if _, err := in.Invoke("init", length, bits); err != nil {
									t.Fatal(err)
								}
								for round := 0; round < 2; round++ {
									for _, name := range []string{"global", "local", "field", "nested", "cast"} {
										out, err := in.Invoke(name, length-1)
										if err != nil || len(out) != 1 || out[0] != want {
											t.Fatalf("%s length%d bits%x:%x,%v", name, length, bits, out, err)
										}
									}
									token, err := in.Invoke("token")
									if err != nil {
										t.Fatal(err)
									}
									out, err := in.Invoke("param", token[0], length-1)
									if err != nil || len(out) != 1 || out[0] != want {
										t.Fatalf("param:%x,%v", out, err)
									}
									if err := in.CollectGC(); err != nil {
										t.Fatal(err)
									}
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestGCGeneralScalarArrayGetTraps(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralScalarArrayGetModule(0x7f, true, true, false))
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
			_, err := in.Invoke("global", index)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
				t.Fatalf("length%d index%d:%v", length, index, err)
			}
			_, err = in.Invoke("null", index)
			trap = nil
			if !errors.As(err, &trap) || trap.Code != TrapNullReference {
				t.Fatalf("null index%d:%v", index, err)
			}
		}
	}
	token, err := in.Invoke("wrong")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("param", token[0], 0); err == nil {
		t.Fatal("wrong structural token accepted")
	}
}
