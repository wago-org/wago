//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wr "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func streamNumericControlCompile(t *testing.T, m *wasm.Module, interruptible bool) CompileOptions {
	t.Helper()
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if !admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp).Eligible {
		t.Fatal("numeric/control fixture not admitted")
	}
	opts := CompileOptions{Interruptible: interruptible, DeferCodeMapping: true}
	if diagnosticsEnabled {
		opts.Stats = &ModuleStats{}
	}
	return opts
}
func streamNumericControlSelected(t *testing.T, opts CompileOptions) {
	t.Helper()
	if diagnosticsEnabled && !regallocCheckEnabled {
		if opts.Stats.Funcs[0].Peephole["streaming-function"] != 1 {
			t.Fatal("numeric/control runtime used legacy fallback")
		}
	}
}
func TestStreamNumericControlAliasesAndCarriers(t *testing.T) {
	old := streamingOperandsEnabled
	streamingOperandsEnabled = true
	defer func() { streamingOperandsEnabled = old }()
	cases := []struct {
		name   string
		params int
		body   []byte
		args   []uint64
		want   uint64
	}{
		{"dirty-i32-unsigned-compare", 2, []byte{0, 0x20, 0, 0x20, 1, 0x49, 0x0b}, []uint64{0xffffffff00000005, 0x8765432100000009}, 1},
		{"dirty-i32-unsigned-division", 2, []byte{0, 0x20, 0, 0x20, 1, 0x6e, 0x0b}, []uint64{0x12345678fffffffe, 0xffffffff00000002}, 0x7fffffff},
		{"select-tee-div-alias", 3, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x22, 0, 0x20, 1, 0x6e, 0x20, 0, 0x6a, 0x0b}, []uint64{21, 3, 1}, 28},
		{"select-tee-div-alias-other", 3, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x22, 0, 0x20, 1, 0x6e, 0x20, 0, 0x6a, 0x0b}, []uint64{21, 3, 0}, 4},
		{"old-local-read-across-div-tee", 3, []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, 0x6e, 0x22, 0, 0x20, 2, 0x1b, 0x20, 0, 0x6a, 0x0b}, []uint64{21, 3, 1}, 28},
		{"old-local-read-across-div-tee-other", 3, []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, 0x6e, 0x22, 0, 0x20, 2, 0x1b, 0x20, 0, 0x6a, 0x0b}, []uint64{21, 3, 0}, 14},
		{"nested-result-branches", 3, []byte{0, 0x41, 6, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x04, 0x7f, 0x41, 7, 0x05, 0x20, 2, 0x41, 2, 0x6e, 0x0b, 0x05, 0x41, 9, 0x0b, 0x6c, 0x0b}, []uint64{1, 0, 20}, 60},
	}
	body := []byte{0}
	for i := 1; i <= 10; i++ {
		body = append(body, 0x20, 0, 0x41, byte(i), 0x6a)
	}
	body = append(body, 0x20, 1, 0x20, 2, 0x6e)
	for i := 0; i < 10; i++ {
		body = append(body, 0x6a)
	}
	body = append(body, 0x0b)
	cases = append(cases, struct {
		name   string
		params int
		body   []byte
		args   []uint64
		want   uint64
	}{"fixed-register-pressure", 3, body, []uint64{5, 99, 3}, 138})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := make([]wasm.ValType, tc.params)
			for i := range ps {
				ps[i] = wasm.I32
			}
			m := mod1(t, ps, []wasm.ValType{wasm.I32}, tc.body)
			for _, intr := range []bool{false, true} {
				opts := streamNumericControlCompile(t, m, intr)
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				streamNumericControlSelected(t, opts)
				if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
					t.Fatalf("interruptible=%v got=%x want=%x", intr, got, tc.want)
				}
			}
		})
	}
}
func TestStreamNumericControlDroppedTrapOrder(t *testing.T) {
	old := streamingOperandsEnabled
	streamingOperandsEnabled = true
	defer func() { streamingOperandsEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, op, min, negone := wasm.I32, byte(0x6d), uint64(0x80000000), uint64(0xffffffff)
		if wide {
			typ, op, min, negone = wasm.I64, 0x7f, 1<<63, ^uint64(0)
		}
		body := []byte{0, 0x20, 0, 0x20, 1, op, 0x1a, 0x00, 0x0b}
		m := modMem(t, 1, []wasm.ValType{typ, typ}, nil, body)
		for _, intr := range []bool{false, true} {
			for _, tc := range []struct {
				a, b   uint64
				code   wr.TrapCode
				offset uint32
			}{{9, 0, wr.TrapDivZero, 4}, {min, negone, wr.TrapDivOverflow, 4}, {9, 3, wr.TrapUnreachable, 6}} {
				opts := streamNumericControlCompile(t, m, intr)
				_, _, err := runMemAmd64WithOptions(t, m, opts, nil, tc.a, tc.b)
				streamNumericControlSelected(t, opts)
				var trap *wr.TrapError
				if !errors.As(err, &trap) || trap.Code != tc.code {
					t.Fatalf("wide=%v intr=%v got=%v want=%v", wide, intr, err, tc.code)
				}
				pc := m.Code[0].LocalDeclBytes + tc.offset
				if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
					t.Fatalf("trap frames=%+v wantpc=%d", trap.Frames, pc)
				}
			}
		}
	}
	// select evaluates both value operands; choosing the other value cannot hide div0.
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, []byte{0, 0x20, 0, 0x20, 1, 0x6d, 0x41, 7, 0x20, 2, 0x1b, 0x1a, 0x00, 0x0b})
	for _, intr := range []bool{false, true} {
		opts := streamNumericControlCompile(t, m, intr)
		_, _, err := runMemAmd64WithOptions(t, m, opts, nil, 9, 0, 0)
		streamNumericControlSelected(t, opts)
		var trap *wr.TrapError
		if !errors.As(err, &trap) || trap.Code != wr.TrapDivZero || len(trap.Frames) != 1 || trap.Frames[0].ProgramCounter != m.Code[0].LocalDeclBytes+4 {
			t.Fatalf("unselected division lost trap/source: %+v", err)
		}
	}
}
