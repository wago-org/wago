//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestSizeIncDecImmediateForms(t *testing.T) {
	tests := []struct {
		name    string
		type_   wasm.ValType
		constOp byte
		op      byte
		imm     byte
		arg     uint64
		want    uint64
	}{
		{"i32 add one", wasm.I32, 0x41, 0x6a, 0x01, 1<<32 - 1, 0},
		{"i32 add minus one", wasm.I32, 0x41, 0x6a, 0x7f, 0, 1<<32 - 1},
		{"i32 sub one", wasm.I32, 0x41, 0x6b, 0x01, 0, 1<<32 - 1},
		{"i32 sub minus one", wasm.I32, 0x41, 0x6b, 0x7f, 1<<32 - 1, 0},
		{"i64 add one", wasm.I64, 0x42, 0x7c, 0x01, ^uint64(0), 0},
		{"i64 add minus one", wasm.I64, 0x42, 0x7c, 0x7f, 0, ^uint64(0)},
		{"i64 sub one", wasm.I64, 0x42, 0x7d, 0x01, 0, ^uint64(0)},
		{"i64 sub minus one", wasm.I64, 0x42, 0x7d, 0x7f, ^uint64(0), 0},
	}
	before := incDecEnabled
	t.Cleanup(func() { incDecEnabled = before })
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{test.type_}, []wasm.ValType{test.type_},
				[]byte{0x00, 0x20, 0x00, test.constOp, test.imm, test.op, 0x0b})
			compile := func(enabled bool) (*encoderamd64.CompiledModule, *ModuleStats) {
				incDecEnabled = enabled
				size := OptimizeSize
				stats := &ModuleStats{}
				cm, err := CompileModuleWith(m, CompileOptions{Objective: &size, Stats: stats})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { cm.CodeImage.Close() })
				return cm, stats
			}
			long, longStats := compile(false)
			short, shortStats := compile(true)
			if got := longStats.Funcs[0].CodeBytes - shortStats.Funcs[0].CodeBytes; got != 1 {
				t.Fatalf("code delta = %d, want 1", got)
			}
			if got := shortStats.Funcs[0].Peephole["inc-dec"]; got != 1 {
				t.Fatalf("inc-dec hits = %d, want 1", got)
			}
			if got := runCompiledAmd64u(t, short, test.arg); got != test.want {
				t.Fatalf("result = %#x, want %#x (long code %d bytes)", got, test.want, len(long.Code))
			}
		})
	}
}

func TestSizeIncDecDirectAdjustments(t *testing.T) {
	before := incDecEnabled
	directBefore := directIncDecEnabled
	t.Cleanup(func() {
		incDecEnabled = before
		directIncDecEnabled = directBefore
	})
	incDecEnabled = true
	directIncDecEnabled = true

	tests := []struct {
		name      string
		objective OptimizationObjective
		increment bool
		want      []byte
		wantHits  int
	}{
		{"size increment", OptimizeSize, true, []byte{0xff, 0xc1}, 1},
		{"embedded decrement", OptimizeEmbedded, false, []byte{0xff, 0xc9}, 1},
		{"balanced add", OptimizeBalanced, true, []byte{0x83, 0xc1, 0x01}, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stats := &CodegenStats{}
			f := fn{
				a:      &encoderamd64.Asm{},
				policy: shared.CodegenPolicyForObjective(currentCodegenPolicy().Selection, test.objective),
				stats:  stats,
			}
			f.unitAdjust(RCX, false, test.increment)
			if got := f.a.B; !bytes.Equal(got, test.want) {
				t.Fatalf("code = % x, want % x", got, test.want)
			}
			if got := stats.Peephole["inc-dec-direct"]; got != test.wantHits {
				t.Fatalf("inc-dec-direct hits = %d, want %d", got, test.wantHits)
			}
		})
	}
}

func TestSizeIncDecDirectRollback(t *testing.T) {
	before := directIncDecEnabled
	t.Cleanup(func() { directIncDecEnabled = before })
	directIncDecEnabled = false
	stats := &CodegenStats{}
	f := fn{
		a:      &encoderamd64.Asm{},
		policy: shared.CodegenPolicyForObjective(currentCodegenPolicy().Selection, OptimizeSize),
		stats:  stats,
	}
	f.unitAdjust(RCX, false, false)
	if want := []byte{0x83, 0xe9, 0x01}; !bytes.Equal(f.a.B, want) {
		t.Fatalf("code = % x, want % x", f.a.B, want)
	}
	if got := stats.Peephole["inc-dec-direct"]; got != 0 {
		t.Fatalf("inc-dec-direct hits = %d, want 0", got)
	}
}
