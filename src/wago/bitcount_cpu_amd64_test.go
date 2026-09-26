//go:build amd64

package wago

import (
	"bytes"
	"math/bits"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
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

func hasBitCountOpcode(code []byte, opcode byte) bool {
	for i := 0; i+3 < len(code); i++ {
		if code[i] != 0xf3 {
			continue
		}
		j := i + 1
		if code[j]&0xf0 == 0x40 {
			j++
		}
		if j+2 < len(code) && code[j] == 0x0f && code[j+1] == opcode {
			return true
		}
	}
	return false
}

func TestAMD64BitCountPaths(t *testing.T) {
	actual := bitCountHostFeaturesSupported
	defer func() { bitCountHostFeaturesSupported = actual }()
	cases := []struct {
		name         string
		op           byte
		width        wasm.ValType
		feature      uint8
		nativeOpcode byte
		inputs       []uint64
		want         []uint64
	}{
		{"i32.clz", 0x67, wasm.I32, shared.BitCountLZCNT, 0xbd,
			[]uint64{0, 1, 1 << 31, 1 << 17}, []uint64{32, 31, 0, 14}},
		{"i64.clz", 0x79, wasm.I64, shared.BitCountLZCNT, 0xbd,
			[]uint64{0, 1, 1 << 63, 1 << 37}, []uint64{64, 63, 0, 26}},
		{"i32.ctz", 0x68, wasm.I32, shared.BitCountTZCNT, 0xbc,
			[]uint64{0, 1, 1 << 31, 1 << 17}, []uint64{32, 0, 31, 17}},
		{"i64.ctz", 0x7a, wasm.I64, shared.BitCountTZCNT, 0xbc,
			[]uint64{0, 1, 1 << 63, 1 << 37}, []uint64{64, 0, 63, 37}},
		{"i32.popcnt", 0x69, wasm.I32, shared.BitCountPOPCNT, 0xb8,
			[]uint64{0, 1, 0xffffffff, 0xaaaaaaaa, 0x55555555, 0x12345678},
			[]uint64{0, 1, 32, 16, 16, uint64(bits.OnesCount32(0x12345678))}},
		{"i64.popcnt", 0x7b, wasm.I64, shared.BitCountPOPCNT, 0xb8,
			[]uint64{0, 1, ^uint64(0), 0xaaaaaaaaaaaaaaaa, 0x5555555555555555, 0x123456789abcdef0},
			[]uint64{0, 1, 64, 32, 32, uint64(bits.OnesCount64(0x123456789abcdef0))}},
	}
	for _, tc := range cases {
		for _, native := range []bool{false, true} {
			name := tc.name + "/fallback"
			if native {
				name = tc.name + "/native"
			}
			t.Run(name, func(t *testing.T) {
				if native && selectedAMD64CompileFeatures(shared.AMD64BitCountRequirements(actual())).BitCountCapabilities()&tc.feature == 0 {
					t.Skip("host cannot execute native instruction")
				}
				mask := uint8(0)
				if native {
					mask = tc.feature
				}
				bitCountHostFeaturesSupported = func() uint8 { return mask }
				compiled, err := Compile(nil, bitCountModule(tc.op, tc.width))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				if compiled.requiredAMD64Features.BitCountCapabilities() != mask {
					t.Fatalf("requirements = %#x, want %#x", compiled.requiredAMD64Features.BitCountCapabilities(), mask)
				}
				if got := hasBitCountOpcode(compiled.code, tc.nativeOpcode); got != native {
					t.Fatalf("native opcode presence = %v, want %v", got, native)
				}
				instance, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				for i, input := range tc.inputs {
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
}

func TestAMD64BitCountArtifactRequirements(t *testing.T) {
	actual := bitCountHostFeaturesSupported
	defer func() { bitCountHostFeaturesSupported = actual }()
	for _, tc := range []struct {
		name    string
		op      byte
		feature uint8
		width   wasm.ValType
	}{
		{"clz", 0x67, shared.BitCountLZCNT, wasm.I32},
		{"ctz", 0x7a, shared.BitCountTZCNT, wasm.I64},
		{"popcnt", 0x69, shared.BitCountPOPCNT, wasm.I32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mask := range []uint8{0, tc.feature} {
				mask = selectedAMD64CompileFeatures(shared.AMD64BitCountRequirements(mask)).BitCountCapabilities()
				bitCountHostFeaturesSupported = func() uint8 { return mask }
				compiled, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(bitCountModule(tc.op, tc.width))
				if err != nil {
					t.Fatal(err)
				}
				if compiled.requiredAMD64Features.BitCountCapabilities() != mask {
					t.Fatalf("compiled requirements = %#x, want %#x", compiled.requiredAMD64Features.BitCountCapabilities(), mask)
				}
				blob, err := compiled.MarshalBinary()
				compiled.Close()
				if err != nil {
					t.Fatal(err)
				}
				bitCountHostFeaturesSupported = func() uint8 { return 0 }
				var loaded Compiled
				err = loaded.UnmarshalBinary(blob)
				if mask != 0 {
					if err == nil || !strings.Contains(err.Error(), "bit-count") {
						t.Fatalf("native artifact on old host: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if loaded.requiredAMD64Features.BitCountCapabilities() != 0 {
						t.Fatal("fallback artifact acquired a CPU requirement")
					}
					loaded.Close()
				}
				bitCountHostFeaturesSupported = func() uint8 { return mask }
				var roundtrip Compiled
				if err := roundtrip.UnmarshalBinary(blob); err != nil {
					t.Fatal(err)
				}
				if roundtrip.requiredAMD64Features.BitCountCapabilities() != mask {
					t.Fatalf("roundtrip requirements = %#x", roundtrip.requiredAMD64Features.BitCountCapabilities())
				}
				roundtrip.Close()
			}
		})
	}
	bitCountHostFeaturesSupported = func() uint8 { return 0 }
	plain, err := Compile(nil, bitCountModule(0x67, wasm.I32))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if bytes.Contains(plain.code, []byte{0xf3, 0x0f, 0xbd}) {
		t.Fatal("fallback contains LZCNT")
	}
}

func TestAMD64BitCountParallelArtifactUnion(t *testing.T) {
	actual := bitCountHostFeaturesSupported
	defer func() { bitCountHostFeaturesSupported = actual }()
	all := selectedAMD64CompileFeatures(shared.AMD64BitCountRequirements(shared.BitCountLZCNT | shared.BitCountTZCNT | shared.BitCountPOPCNT)).BitCountCapabilities()
	bitCountHostFeaturesSupported = func() uint8 { return all }
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clz", 0, 0), wasmtest.ExportEntry("ctz", 0, 1), wasmtest.ExportEntry("popcnt", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x67, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x68, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x69, 0x0b}),
		)),
	)
	compiled, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(3).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.requiredAMD64Features.BitCountCapabilities() != all {
		t.Fatalf("parallel requirements = %#x, want %#x", compiled.requiredAMD64Features.BitCountCapabilities(), all)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	bitCountHostFeaturesSupported = func() uint8 { return all &^ shared.BitCountTZCNT }
	var loaded Compiled
	err = loaded.UnmarshalBinary(blob)
	if all == 0 {
		if err != nil {
			t.Fatal(err)
		}
		loaded.Close()
	} else if err == nil || !strings.Contains(err.Error(), "bit-count") {
		t.Fatalf("partially capable host loaded artifact: %v", err)
	}
}

func BenchmarkAMD64BitCountCompile(b *testing.B) {
	actual := bitCountHostFeaturesSupported
	defer func() { bitCountHostFeaturesSupported = actual }()
	for _, tc := range []struct {
		name    string
		op      byte
		feature uint8
	}{
		{"clz", 0x67, shared.BitCountLZCNT},
		{"ctz", 0x68, shared.BitCountTZCNT},
		{"popcnt", 0x69, shared.BitCountPOPCNT},
	} {
		module := bitCountModule(tc.op, wasm.I32)
		for _, native := range []bool{false, true} {
			name := tc.name + "/fallback"
			if native {
				name = tc.name + "/native"
			}
			b.Run(name, func(b *testing.B) {
				mask := uint8(0)
				if native {
					mask = tc.feature
				}
				bitCountHostFeaturesSupported = func() uint8 { return mask }
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					compiled, err := Compile(nil, module)
					if err != nil {
						b.Fatal(err)
					}
					compiled.Close()
				}
			})
		}
	}
}

func BenchmarkAMD64BitCountInvoke(b *testing.B) {
	for _, tc := range []struct {
		name    string
		op      byte
		width   wasm.ValType
		feature uint8
	}{
		{"clz", 0x67, wasm.I32, shared.BitCountLZCNT},
		{"ctz", 0x68, wasm.I32, shared.BitCountTZCNT},
		{"popcnt", 0x69, wasm.I32, shared.BitCountPOPCNT},
	} {
		for _, native := range []bool{false, true} {
			name := tc.name + "/fallback"
			if native {
				name = tc.name + "/native"
			}
			b.Run(name, func(b *testing.B) {
				actual := bitCountHostFeaturesSupported
				defer func() { bitCountHostFeaturesSupported = actual }()
				if native && selectedAMD64CompileFeatures(shared.AMD64BitCountRequirements(actual())).BitCountCapabilities()&tc.feature == 0 {
					b.Skip("host cannot execute native instruction")
				}
				mask := uint8(0)
				if native {
					mask = tc.feature
				}
				bitCountHostFeaturesSupported = func() uint8 { return mask }
				compiled, err := Compile(nil, bitCountModule(tc.op, tc.width))
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
					if _, err := instance.Invoke("f", 0x12345678); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(compiled.code)), "code-B")
			})
		}
	}
}
