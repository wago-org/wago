//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIntegerInitializerBoundsAndTypes(t *testing.T) {
	resolve := func(i uint32) (wasm.ValType, bool) {
		if i == 0 || i == 255 {
			return wasm.I32, true
		}
		if i == 1 {
			return wasm.I64, true
		}
		return wasm.F32, i == 2
	}
	var depth8, depth9 []byte
	for range 8 {
		depth8 = append(depth8, 0x41, 1)
	}
	depth9 = append(append([]byte(nil), depth8...), 0x41, 1)
	for range 7 {
		depth8 = append(depth8, 0x6a)
	}
	depth8 = append(depth8, 0x0b)
	for range 8 {
		depth9 = append(depth9, 0x6a)
	}
	depth9 = append(depth9, 0x0b)
	length128 := []byte{0x41, 0x81, 0x80, 0}
	length129 := []byte{0x41, 1}
	for range 41 {
		length128 = append(length128, 0x41, 0, 0x6a)
		length129 = append(length129, 0x41, 0, 0x6a)
	}
	length128 = append(length128, 0x0b)
	length129 = append(length129, 0x41, 0, 0x6a, 0x0b)
	if len(length128) != 128 || len(length129) != 129 {
		t.Fatal("byte boundary fixture")
	}
	for _, tc := range []struct {
		name  string
		body  []byte
		typ   wasm.ValType
		valid bool
	}{
		{"depth8", depth8, wasm.I32, true}, {"depth9", depth9, wasm.I32, false}, {"bytes128", length128, wasm.I32, true}, {"bytes129", length129, wasm.I32, false},
		{"index255", []byte{0x23, 0xff, 1, 0x41, 1, 0x6b, 0x0b}, wasm.I32, true},
		{"i64", []byte{0x23, 1, 0x42, 3, 0x7e, 0x42, 2, 0x7d, 0x0b}, wasm.I64, true},
		{"mixed", []byte{0x23, 1, 0x41, 3, 0x6a, 0x0b}, wasm.I32, false},
		{"same-width", []byte{0x23, 2, 0x41, 3, 0x6a, 0x0b}, wasm.I32, false},
		{"unavailable", []byte{0x23, 3, 0x0b}, wasm.I32, false},
		{"underflow", []byte{0x41, 1, 0x6a, 0x0b}, wasm.I32, false},
		{"multi-result", []byte{0x41, 1, 0x41, 2, 0x0b}, wasm.I32, false},
		{"wrong-result", []byte{0x41, 1, 0x0b}, wasm.I64, false},
		{"trailing", []byte{0x41, 1, 0x0b, 0x0b}, wasm.I32, false},
		{"missing-end", []byte{0x41, 1}, wasm.I32, false},
		{"truncated", []byte{0x41, 0x80}, wasm.I32, false},
		{"division", []byte{0x41, 1, 0x41, 1, 0x6d, 0x0b}, wasm.I32, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateIntegerInitializer(tc.body, tc.typ, resolve); (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
	for _, body := range [][]byte{depth8, length128, depth9, length129} {
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, body))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x0b}))))
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		pressure := 0
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
		valid := len(body) == len(depth8) || len(body) == len(length128)
		if (err == nil) != valid || (image != nil) != valid || !valid && pressure != 0 {
			t.Fatal("public staging boundary", len(body), err, pressure)
		}
		if image != nil && image.CodeImage != nil {
			image.CodeImage.Close()
		}
		if image, err := CompileProduction(source, ProductionOptions{DeferCodeMapping: true}); image != nil || err == nil {
			if image != nil && image.CodeImage != nil {
				image.CodeImage.Close()
			}
			t.Fatal("private expression profile widened")
		}
	}
}
