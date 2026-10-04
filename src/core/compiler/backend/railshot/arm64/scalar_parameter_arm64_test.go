//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSharedScalarLeafMatchesEstablishedCodeArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	previous := sharedScalarEnabled
	defer func() { sharedScalarEnabled = previous }()
	for _, wide := range []bool{false, true} {
		typ, constant, add := wasm.I32, byte(0x41), byte(0x6a)
		if wide {
			typ, constant, add = wasm.I64, 0x42, 0x7c
		}
		m := modFuncs(t, funcDef{params: []wasm.ValType{typ}, results: []wasm.ValType{typ}, body: []byte{0, 0x20, 0, constant, 1, add, 0x0b}})
		sharedScalarEnabled = false
		baseline, err := CompileModuleWith(m, CompileOptions{Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		sharedScalarEnabled = true
		var stats ModuleStats
		candidate, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats})
		if err != nil {
			baseline.CodeImage.Close()
			t.Fatal(err)
		}
		if !stats.Funcs[0].SharedScalar || stats.Funcs[0].FrameBytes != 0 || stats.Funcs[0].ScalarSpills != 0 || stats.Funcs[0].ScalarReloads != 0 {
			t.Fatalf("wide=%v: shared leaf has frame or spills: %+v", wide, stats.Funcs[0])
		}
		if !bytes.Equal(baseline.Code, candidate.Code) {
			t.Fatalf("wide=%v established=%x shared=%x", wide, baseline.Code, candidate.Code)
		}
		baseline.CodeImage.Close()
		candidate.CodeImage.Close()
	}
}

func TestSharedScalarFrameAgreementsAndPolicyArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	previous := sharedScalarEnabled
	sharedScalarEnabled = true
	defer func() { sharedScalarEnabled = previous }()
	for _, tc := range []struct {
		name      string
		body      []byte
		options   map[string]bool
		wantFrame bool
	}{
		{"unused-zero", []byte{1, 1, 0x7f, 0x20, 0, 0x41, 1, 0x6a, 0x0b}, nil, false},
		{"elision-disabled", []byte{1, 1, 0x7f, 0x20, 0, 0x41, 1, 0x6a, 0x0b}, map[string]bool{"frame-elide-reghomed": false}, true},
		{"local-only-agreement", []byte{0, 0x20, 0, 0x04, 0x40, 0x41, 7, 0x21, 0, 0x05, 0x41, 9, 0x21, 0, 0x0b, 0x20, 0, 0x0b}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: tc.body})
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats, Optimizations: tc.options})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			s := stats.Funcs[0]
			if !s.SharedScalar || (s.FrameBytes > 0) != tc.wantFrame {
				t.Fatalf("stats=%+v want frame=%v", s, tc.wantFrame)
			}
			if tc.name == "local-only-agreement" && s.MaxSpillSlots != 0 {
				t.Fatalf("agreement proof unexpectedly used spills: %+v", s)
			}
		})
	}
}
