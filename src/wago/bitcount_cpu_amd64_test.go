//go:build amd64

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func bitCountModule(op byte, width wasm.ValType) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{width}, []wasm.ValType{width}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, op, 0x0b}))),
	)
}

func TestAMD64BitCountHostGate(t *testing.T) {
	old := bitCountHostFeaturesSupported
	bitCountHostFeaturesSupported = func() bool { return false }
	defer func() { bitCountHostFeaturesSupported = old }()

	for _, tc := range []struct {
		name string
		op   byte
	}{
		{name: "clz", op: 0x67},
		{name: "ctz", op: 0x68},
		{name: "popcnt", op: 0x69},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(nil, bitCountModule(tc.op, wasm.I32))
			if compiled != nil {
				defer compiled.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "bit-count") {
				t.Fatalf("compile %s without bit-count CPU features = %v, want error", tc.name, err)
			}
		})
	}
}

func TestAMD64BitCountArtifactHostGate(t *testing.T) {
	if !hostSupportsAMD64BitCount() {
		t.Skip("host lacks bit-count CPU features")
	}
	compiled, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(bitCountModule(0x68, wasm.I32))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	data, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	old := bitCountHostFeaturesSupported
	bitCountHostFeaturesSupported = func() bool { return false }
	defer func() { bitCountHostFeaturesSupported = old }()
	var loaded Compiled
	if err := loaded.UnmarshalBinary(data); err == nil || !strings.Contains(err.Error(), "bit-count") {
		t.Fatalf("load without bit-count CPU features = %v, want error", err)
	}
}

func TestAMD64BitCountEdges(t *testing.T) {
	if !hostSupportsAMD64BitCount() {
		t.Skip("host lacks bit-count CPU features")
	}
	for _, tc := range []struct {
		name  string
		op    byte
		width wasm.ValType
		want  [3]uint64
	}{
		{name: "i32.clz", op: 0x67, width: wasm.I32, want: [3]uint64{32, 31, 0}},
		{name: "i32.ctz", op: 0x68, width: wasm.I32, want: [3]uint64{32, 0, 31}},
		{name: "i32.popcnt", op: 0x69, width: wasm.I32, want: [3]uint64{0, 1, 1}},
		{name: "i64.clz", op: 0x79, width: wasm.I64, want: [3]uint64{64, 63, 0}},
		{name: "i64.ctz", op: 0x7a, width: wasm.I64, want: [3]uint64{64, 0, 63}},
		{name: "i64.popcnt", op: 0x7b, width: wasm.I64, want: [3]uint64{0, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(nil, bitCountModule(tc.op, tc.width))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			top := uint64(1) << 31
			if tc.width == wasm.I64 {
				top = uint64(1) << 63
			}
			for i, input := range [3]uint64{0, 1, top} {
				got, err := instance.Invoke("f", input)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != 1 || got[0] != tc.want[i] {
					t.Fatalf("f(%#x) = %v, want %d", input, got, tc.want[i])
				}
			}
		})
	}
}

func BenchmarkAMD64BitCountCompile(b *testing.B) {
	module := bitCountModule(0x68, wasm.I32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(nil, module)
		if err != nil {
			b.Fatal(err)
		}
		compiled.Close()
	}
}

func BenchmarkAMD64BitCountInvoke(b *testing.B) {
	compiled, err := Compile(nil, bitCountModule(0x68, wasm.I32))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := instance.Invoke("f", 8); err != nil {
			b.Fatal(err)
		}
	}
}
