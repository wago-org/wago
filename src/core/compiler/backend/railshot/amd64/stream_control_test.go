//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

// Positive wired-entry gate. ON must reach the new whole-function engine, not
// merely pass via the legacy fallback. Runtime expectations are independent.
func TestStreamControlBasicResultIf(t *testing.T) {
	old := streamingOperandsEnabled
	defer func() { streamingOperandsEnabled = old }()
	body := []byte{0, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x41, 3, 0x6a, 0x05, 0x20, 2, 0x41, 4, 0x6b, 0x0b, 0x41, 2, 0x6c, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if !admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp).Eligible {
		t.Fatal("result-if fixture not admitted")
	}
	for _, on := range []bool{false, true} {
		streamingOperandsEnabled = on
		for _, tc := range []struct{ cond, want uint64 }{{0, 32}, {1, 24}, {0xffffffff, 24}} {
			if got := runAmd64u(t, m, tc.cond, 9, 20); got != tc.want {
				t.Fatalf("enabled=%v cond=%x got=%d want=%d", on, tc.cond, got, tc.want)
			}
		}
		if diagnosticsEnabled {
			stats := compileWithStats(t, m, false)
			n := stats.Funcs[0].Peephole["streaming-function"]
			want := 0
			if on && !regallocCheckEnabled {
				want = 1
			}
			if n != want {
				t.Fatalf("enabled=%v streaming-function=%d want%d", on, n, want)
			}
		}
	}
}

type streamingControlCase struct {
	name   string
	params []wasm.ValType
	body   []byte
	args   []uint64
	want   uint64
}

func streamingControlCases() []streamingControlCase {
	return []streamingControlCase{
		{"if-result-then", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []byte{0, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x41, 3, 0x6a, 0x05, 0x20, 2, 0x41, 4, 0x6b, 0x0b, 0x41, 2, 0x6c, 0x0b}, []uint64{1, 9, 20}, 24},
		{"if-result-else", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []byte{0, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x41, 3, 0x6a, 0x05, 0x20, 2, 0x41, 4, 0x6b, 0x0b, 0x41, 2, 0x6c, 0x0b}, []uint64{0, 9, 20}, 32},
		{"br-if-result-taken", []wasm.ValType{wasm.I32}, []byte{0, 0x02, 0x7f, 0x41, 41, 0x20, 0, 0x0d, 0, 0x1a, 0x41, 7, 0x0b, 0x0b}, []uint64{1}, 41},
		{"br-if-result-not-taken", []wasm.ValType{wasm.I32}, []byte{0, 0x02, 0x7f, 0x41, 41, 0x20, 0, 0x0d, 0, 0x1a, 0x41, 7, 0x0b, 0x0b}, []uint64{0}, 7},
		{"loop-local-joins", []wasm.ValType{wasm.I32}, []byte{1, 2, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1, 0x20, 2, 0x20, 0, 0x6a, 0x21, 2, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}, []uint64{20}, 210},
		{"loop-zero-trip", []wasm.ValType{wasm.I32}, []byte{1, 2, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1, 0x20, 2, 0x20, 0, 0x6a, 0x21, 2, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}, []uint64{0}, 0},
		{"branch-to-function-result", nil, []byte{0, 0x41, 9, 0x0c, 0, 0, 0x0b}, nil, 9},
		{"early-return-with-deeper-value", nil, []byte{0, 0x41, 3, 0x41, 11, 0x0f, 0x0b}, nil, 11},
		{"join-preserves-older-value", []wasm.ValType{wasm.I32}, []byte{0, 0x41, 6, 0x20, 0, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x6c, 0x0b}, []uint64{0}, 54},
	}
}

func TestStreamControlStructured(t *testing.T) {
	old := streamingOperandsEnabled
	defer func() { streamingOperandsEnabled = old }()
	for _, tc := range streamingControlCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			if tc.name == "loop-local-joins" || tc.name == "loop-zero-trip" {
				body = nil
				for _, op := range tc.body {
					if op == 0x45 {
						body = append(body, 0x41, 0, 0x46)
					} else {
						body = append(body, op)
					}
				}
			}
			m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, body)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			if !admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp).Eligible {
				t.Fatal("structured fixture not admitted")
			}
			for _, on := range []bool{false, true} {
				streamingOperandsEnabled = on
				if got := runAmd64u(t, m, tc.args...); got != tc.want {
					t.Fatalf("enabled=%v got=%d want=%d", on, got, tc.want)
				}
				if diagnosticsEnabled {
					stats := compileWithStats(t, m, false)
					want := 0
					if on && !regallocCheckEnabled {
						want = 1
					}
					if got := stats.Funcs[0].Peephole["streaming-function"]; got != want {
						t.Fatalf("selected=%d want=%d", got, want)
					}
				}
			}
		})
	}
}

func TestStreamControlInterruptible(t *testing.T) {
	old := streamingOperandsEnabled
	streamingOperandsEnabled = true
	defer func() { streamingOperandsEnabled = old }()
	var poll encoder.Asm
	poll.Load64(RSI, RBX, -offTrapCellPtr)
	poll.Load32(RSI, RSI, 0)
	poll.TestSelf(RSI, false)
	for _, tc := range streamingControlCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			loop := tc.name == "loop-local-joins" || tc.name == "loop-zero-trip"
			if loop {
				body = nil
				for _, op := range tc.body {
					if op == 0x45 {
						body = append(body, 0x41, 0, 0x46)
					} else {
						body = append(body, op)
					}
				}
			}
			m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, body)
			opts := CompileOptions{Interruptible: true, DeferCodeMapping: true}
			stats := &ModuleStats{}
			if diagnosticsEnabled {
				opts.Stats = stats
			}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled && !regallocCheckEnabled && stats.Funcs[0].Peephole["streaming-function"] != 1 {
				t.Fatal("interruptible fixture not selected")
			}
			if loop && !bytes.Contains(cm.Code, poll.B) {
				t.Fatal("loop interrupt poll absent")
			}
			if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
				t.Fatalf("interruptible got=%d want=%d", got, tc.want)
			}
			if loop {
				off, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
				if err != nil {
					t.Fatal(err)
				}
				if off.CodeImage != nil {
					defer off.CodeImage.Close()
				}
				if bytes.Contains(off.Code, poll.B) {
					t.Fatal("ordinary loop unexpectedly polls")
				}
			}
		})
	}
}
