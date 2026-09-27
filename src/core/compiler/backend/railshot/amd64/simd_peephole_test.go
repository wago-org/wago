//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDPeepholesFire(t *testing.T) {
	v128 := []wasm.ValType{wasm.V128}
	unpinnedParams := make([]wasm.ValType, 12)
	for i := range unpinnedParams {
		unpinnedParams[i] = wasm.V128
	}
	unpinnedTee := []byte{0x01, 0x01, 0x7b} // one declared v128 local (index 12)
	for i := byte(0); i < 12; i++ {
		for range 3 {
			unpinnedTee = append(unpinnedTee, 0x20, i, 0x1a) // keep params hotter than local 12
		}
	}
	unpinnedTee = append(unpinnedTee,
		0x20, 0x00, 0x22, 0x0c,
		0x20, 0x0c, 0xfd, 0x4e,
		0x0b,
	)
	cases := []struct {
		name   string
		params []wasm.ValType
		body   []byte
		peep   string
	}{
		{
			name: "constant shift", params: v128, peep: "simd-shift-imm",
			body: []byte{
				0x00,
				0x20, 0x00, 0x41, 0x07, 0xfd, 0xab, 0x01,
				0x0b,
			},
		},
		{
			name: "native shuffle", params: []wasm.ValType{wasm.V128, wasm.V128}, peep: "simd-shuffle-native",
			body: []byte{
				0x00,
				0x20, 0x00, 0x20, 0x01, 0xfd, 0x0d,
				0, 1, 2, 3, 16, 17, 18, 19, 4, 5, 6, 7, 20, 21, 22, 23,
				0x0b,
			},
		},
		{
			name: "live tee forwarding", params: unpinnedParams, peep: "simd-local-forward",
			body: unpinnedTee,
		},
		{
			name: "rotate expansion", params: v128, peep: "simd-rotr-imm",
			body: []byte{
				0x01, 0x01, 0x7b,
				0x20, 0x00, 0x22, 0x01,
				0x41, 0x07, 0xfd, 0xad, 0x01,
				0x20, 0x01, 0x41, 0x19, 0xfd, 0xab, 0x01,
				0xfd, 0x50,
				0x0b,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := compileWithStats(t, mod1(t, tc.params, v128, tc.body), false).Funcs[0]
			if got := s.Peephole[tc.peep]; got == 0 {
				t.Fatalf("%s did not fire (all: %v)", tc.peep, s.Peephole)
			}
		})
	}
}

func TestAVX512PackedRotateFusionAMD64(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, []byte{
		0x01, 0x01, 0x7b,
		0x20, 0x00, 0x22, 0x01,
		0x41, 0x07, 0xfd, 0xad, 0x01,
		0x20, 0x01, 0x41, 0x19, 0xfd, 0xab, 0x01,
		0xfd, 0x50, 0x0b,
	})
	optimizations := map[string]bool{"avx512-vrotate": true}
	var input, want [16]byte
	for i := 0; i < 4; i++ {
		v := uint32(0x12345678) ^ uint32(i)*0x9e3779b9
		binary.LittleEndian.PutUint32(input[i*4:], v)
		binary.LittleEndian.PutUint32(want[i*4:], bits.RotateLeft32(v, -7))
	}
	features := shared.AMD64ModernBaseline | shared.AMD64AVX512
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Stats: &stats, AMD64FeaturesSet: true, AMD64Features: features,
		Optimizations: optimizations,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["simd-rotr-avx512"]; got != 1 || !cm.RequiresAVX512 {
		t.Fatalf("rotate fusions=%d requiresAVX512=%v", got, cm.RequiresAVX512)
	}
	if got := runAmd64V128WithOptions(t, m, &input, CompileOptions{
		AMD64FeaturesSet: true, AMD64Features: 0, Optimizations: optimizations,
	}); got != want {
		t.Fatalf("baseline result=%x, want %x", got, want)
	}
	if linuxHostHasAVX512VL() {
		if got := runAmd64V128WithOptions(t, m, &input, CompileOptions{
			AMD64FeaturesSet: true, AMD64Features: features, Optimizations: optimizations,
		}); got != want {
			t.Fatalf("AVX-512 result=%x, want %x", got, want)
		}
	}
}
