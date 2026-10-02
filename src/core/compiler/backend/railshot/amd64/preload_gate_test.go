//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestConstantPreloadScanGates(t *testing.T) {
	tests := []struct {
		name      string
		body      []byte
		wantFloat int
		wantSIMD  int
	}{
		{name: "integer", body: []byte{0x00, 0x41, 1, 0x1a, 0x0b}},
		{name: "float", body: []byte{0x00, 0x43, 0, 0, 0x80, 0x3f, 0x1a, 0x0b}, wantFloat: 1},
		{name: "simd", body: append([]byte{0x00, 0xfd, 12}, append(make([]byte, 16), 0x1a, 0x0b)...), wantSIMD: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, nil, tc.body)
			s := compileWithStats(t, m, false).Funcs[0]
			if got := s.Peephole["float-preload-scan"]; got != tc.wantFloat {
				t.Fatalf("float preload scans = %d, want %d (all %v)", got, tc.wantFloat, s.Peephole)
			}
			if got := s.Peephole["v128-preload-scan"]; got != tc.wantSIMD {
				t.Fatalf("SIMD preload scans = %d, want %d (all %v)", got, tc.wantSIMD, s.Peephole)
			}
		})
	}
}

func TestConstantPreloadScanGatesKeepCodeIdentity(t *testing.T) {
	modules := []*wasm.Module{
		mod1(t, nil, nil, []byte{0x00, 0x41, 1, 0x1a, 0x0b}),
		mod1(t, nil, nil, []byte{0x00, 0x43, 0, 0, 0x80, 0x3f, 0x1a, 0x0b}),
		mod1(t, nil, nil, append([]byte{0x00, 0xfd, 12}, append(make([]byte, 16), 0x1a, 0x0b)...)),
	}
	defer func(prev bool) { preloadScanGatesEnabled = prev }(preloadScanGatesEnabled)
	for i, m := range modules {
		preloadScanGatesEnabled = false
		base, err := CompileModule(m)
		if err != nil {
			t.Fatal(err)
		}
		preloadScanGatesEnabled = true
		got, err := CompileModule(m)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Code, base.Code) {
			t.Fatalf("module %d native code changed", i)
		}
	}
}
