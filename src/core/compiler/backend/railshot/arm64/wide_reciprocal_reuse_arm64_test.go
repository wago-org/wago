//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestWideReciprocalReuseWriteAndOwnershipProof(t *testing.T) {
	old := wideReciprocalReuseEnabled
	wideReciprocalReuseEnabled = true
	t.Cleanup(func() { wideReciprocalReuseEnabled = old })
	for _, tc := range []struct {
		name   string
		change func(*fn) regMask
		want   bool
	}{
		{"unchanged", func(f *fn) regMask { return 0 }, true},
		{"multiply reads reciprocal", func(f *fn) regMask { f.a.Umulh(X10, X10, X9); return 0 }, true},
		{"multiply overwrites reciprocal", func(f *fn) regMask { f.a.Umulh(X9, X10, X9); return 0 }, false},
		{"W write clobbers X", func(f *fn) regMask { f.a.MovImm32(X9, 7); return 0 }, false},
		{"load clobbers X", func(f *fn) regMask { f.a.Load64(X9, SP, 0); return 0 }, false},
		{"unrelated write", func(f *fn) regMask { f.a.MovImm32(X10, 7); return 0 }, true},
		{"branch", func(f *fn) regMask { f.a.Branch(); return 0 }, false},
		{"return", func(f *fn) regMask { f.a.Ret(); return 0 }, false},
		{"window boundary", func(f *fn) regMask {
			for range 12 {
				f.a.Nop()
			}
			return 0
		}, true},
		{"window exceeded", func(f *fn) regMask {
			for range 13 {
				f.a.Nop()
			}
			return 0
		}, false},
		{"rewind", func(f *fn) regMask { f.wideReciprocalEnd += 4; return 0 }, false},
		{"misaligned", func(f *fn) regMask { f.wideReciprocalEnd--; return 0 }, false},
		{"avoid", func(f *fn) regMask { return maskOf(X9) }, false},
		{"pinned", func(f *fn) regMask { f.pinned = maskOf(X9); return 0 }, false},
		{"reserved", func(f *fn) regMask { f.reserved = maskOf(X9); return 0 }, false},
		{"local home", func(f *fn) regMask { f.pinnedLocalMask = maskOf(X9); return 0 }, false},
		{"owned", func(f *fn) regMask { f.pushReg(X9, mtI64); return 0 }, false},
		{"invalid register", func(f *fn) regMask { f.wideReciprocalReg = regNone; return 0 }, false},
		{"different bits", func(f *fn) regMask { f.wideReciprocalBits++; return 0 }, false},
		{"source boundary", func(f *fn) regMask { f.wideReciprocalEnd = 0; return 0 }, false},
		{"inactive sink", func(f *fn) regMask { f.wideReciprocalActive = false; return 0 }, false},
		{"calls", func(f *fn) regMask { f.makesCalls = true; return 0 }, false},
		{"exception handlers", func(f *fn) regMask { f.moduleEH = true; return 0 }, false},
		{"streaming", func(f *fn) regMask { f.streamingReduction = true; return 0 }, false},
		{"select group", func(f *fn) regMask { f.selectGroupEnd = 1; return 0 }, false},
		{"inline locals", func(f *fn) regMask { f.localBase = 1; return 0 }, false},
		{"inline forwarding", func(f *fn) regMask { f.inlineForwardN = 1; return 0 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := gpClassEmitter()
			value := wideReciprocalU32(97)
			f.a.MovImm64(X9, value)
			f.wideReciprocalBits, f.wideReciprocalEnd, f.wideReciprocalReg = value, uint32(f.a.Len()), X9
			f.wideReciprocalActive = true
			avoid := tc.change(f)
			before := f.a.Len()
			reg, ok := f.reuseWideReciprocal(value, avoid)
			if ok != tc.want || ok && reg != X9 || f.a.Len() != before {
				t.Fatalf("reg=%d reuse=%v want=%v bytes=%x", reg, ok, tc.want, f.a.B)
			}
		})
	}
}

func TestWideReciprocalReuseExecutionAndJoin(t *testing.T) {
	old := wideReciprocalReuseEnabled
	t.Cleanup(func() { wideReciprocalReuseEnabled = old })
	for _, join := range []bool{false, true} {
		// A local assignment sinks the expression in the ordinary backend.
		body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a}
		quotient := []byte{0x20, 0, 0x41}
		quotient = append(quotient, wasmtest.SLEB32(97)...)
		quotient = append(quotient, 0x6e)
		if join {
			body = append(body, 0x20, 1, 0x04, 0x40)
			body = append(body, quotient...)
			body = append(body, 0x21, 2, 0x0b)
		}
		body = append(body, quotient...)
		body = append(body, quotient...)
		body = append(body, 0x6a, 0x21, 2, 0x20, 2, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, enabled := range []bool{false, true} {
			wideReciprocalReuseEnabled = enabled
			for _, n := range []uint32{0, 1, 96, 97, 98, 0x7fffffff, 0x80000000, 0xffffffff} {
				for _, flag := range []uint64{0, 1} {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"linear-integer-tree": false, "loop-int-const": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, uint64(n)|0xdeadbeef00000000, flag)
					if want := uint64((n / 97) * 2); err != nil || got != want {
						t.Fatalf("join=%v enabled=%v n=%x flag=%d got=%x err=%v want=%x", join, enabled, n, flag, got, err, want)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["wide-reciprocal-reuse"] > 0) != enabled {
						t.Fatalf("enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
