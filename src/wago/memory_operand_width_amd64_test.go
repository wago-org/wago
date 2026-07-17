//go:build amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func dirtyMemoryOperationsModule() []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	zero := append(append(wasmtest.Name("env"), wasmtest.Name("zero")...), 0x00, 0x00)
	one := append(append(wasmtest.Name("env"), wasmtest.Name("one")...), 0x00, 0x00)
	bodies := [][]byte{
		// Preserve the dirty host result through a local before scalar addressing.
		{0x01, 0x01, 0x7f, 0x10, 0x00, 0x21, 0x00, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b},
		// store8 at dirty low-bit address 1, then read it back.
		{0x10, 0x01, 0x41, 0x07, 0x3a, 0x00, 0x00, 0x41, 0x01, 0x2d, 0x00, 0x00, 0x0b},
		// fill one byte at dirty low-bit offset zero.
		{0x10, 0x00, 0x41, 0x09, 0x10, 0x01, 0xfc, 0x0b, 0x00, 0x41, 0x00, 0x2d, 0x00, 0x00, 0x0b},
		// Seed byte 1, then copy one byte to dirty low-bit destination zero.
		{0x41, 0x01, 0x41, 0x08, 0x3a, 0x00, 0x00, 0x10, 0x00, 0x41, 0x01, 0x10, 0x01, 0xfc, 0x0a, 0x00, 0x00, 0x41, 0x00, 0x2d, 0x00, 0x00, 0x0b},
		// Initialize one byte from passive data using dirty destination/source/length.
		{0x10, 0x00, 0x10, 0x00, 0x10, 0x01, 0xfc, 0x08, 0x01, 0x00, 0x41, 0x00, 0x2d, 0x00, 0x00, 0x0b},
	}
	codes := make([][]byte, len(bodies))
	for i := range bodies {
		if i == 0 {
			codes[i] = append(wasmtest.ULEB(uint32(len(bodies[i]))), bodies[i]...)
		} else {
			codes[i] = wasmtest.Code(bodies[i])
		}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(zero, one)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0),
		)),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("load", 0, 2),
			wasmtest.ExportEntry("store", 0, 3),
			wasmtest.ExportEntry("fill", 0, 4),
			wasmtest.ExportEntry("copy", 0, 5),
			wasmtest.ExportEntry("init", 0, 6),
		)),
		wasmtest.Section(12, wasmtest.ULEB(2)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
		wasmtest.Section(11, wasmtest.Vec(
			[]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x2a},
			[]byte{0x01, 0x01, 0x2a},
		)),
	)
}

func TestMemory32OperationsCanonicalizeDirtySynchronousHostResults(t *testing.T) {
	compiled := MustCompile(dirtyMemoryOperationsModule())
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		want int32
	}{
		{name: "load", want: 42},
		{name: "store", want: 7},
		{name: "fill", want: 9},
		{name: "copy", want: 8},
		{name: "init", want: 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
				"env.zero": HostFunc(func(_ HostModule, _, results []uint64) { results[0] = 0xdead_beef_0000_0000 }),
				"env.one":  HostFunc(func(_ HostModule, _, results []uint64) { results[0] = 0xcafe_babe_0000_0001 }),
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke(tc.name)
			if err != nil || len(got) != 1 || AsI32(got[0]) != tc.want {
				t.Fatalf("%s = %v, %v; want %d", tc.name, got, err, tc.want)
			}
		})
	}

	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.zero": HostFunc(func(_ HostModule, _, results []uint64) { results[0] = 0xdead_beef_0001_0000 }),
		"env.one":  HostFunc(func(_ HostModule, _, results []uint64) { results[0] = 1 }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("load"); err == nil {
		t.Fatal("out-of-range low i32 address did not trap")
	}
}
