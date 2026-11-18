//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wr "github.com/wago-org/wago/src/core/runtime"
	"reflect"
	"testing"
)

func TestStreamLifetimeEventPCs(t *testing.T) {
	old := streamingLocalLifetimesEnabled
	defer func() { streamingLocalLifetimesEnabled = old }()
	cases := []struct {
		params []wasm.ValType
		body   []byte
		events []uint32
	}{
		{[]wasm.ValType{wasm.I32}, []byte{0, 0x20, 0x80, 0, 0x41, 1, 0x6a, 0x22, 0x80, 0, 0x1a, 0x20, 0, 0x0b}, []uint32{streamEventDead, 6 | streamEventWrite, 10 | streamEventDead}},
		{nil, []byte{1, 0x81, 1, 0x7f, 0x41, 17, 0x21, 0x80, 1, 0x20, 0x80, 0x81, 0, 0x0b}, []uint32{2 | 128<<16 | streamEventWrite, 5 | 128<<16 | streamEventDead}},
	}
	for _, tc := range cases {
		m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, tc.body)
		if e := wasm.ValidateModule(m); e != nil {
			t.Fatal(e)
		}
		streamingLocalLifetimesEnabled = true
		s := admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp)
		if !s.Eligible || !reflect.DeepEqual(s.LocalEvents, tc.events) {
			t.Fatalf("events%x want%x", s.LocalEvents, tc.events)
		}
		streamingLocalLifetimesEnabled = false
		s = admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp)
		if s.LocalEvents != nil {
			t.Fatal("disabled events")
		}
	}
	streamingLocalLifetimesEnabled = true
	for _, body := range [][]byte{
		{0, 0x20, 0, 0x02, 0x40, 0x01, 0x0b, 0x0b},
		{0, 0x20, 0, 0x03, 0x40, 0x01, 0x0b, 0x0b},
		{0, 0x20, 0, 0x0f, 0x0b},
		{0, 0x20, 0, 0x1a, 0x00, 0x0b},
	} {
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		if e := wasm.ValidateModule(m); e != nil {
			t.Fatal(e)
		}
		s := admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp)
		if !s.Eligible || s.LocalEvents != nil {
			t.Fatal("control body retained events")
		}
	}
}

func TestStreamLifetimeVersions(t *testing.T) {
	oldStream, oldLife, oldSink := streamingOperandsEnabled, streamingLocalLifetimesEnabled, streamingHomeSinkEnabled
	streamingOperandsEnabled = true
	streamingHomeSinkEnabled = false
	defer func() {
		streamingOperandsEnabled, streamingLocalLifetimesEnabled, streamingHomeSinkEnabled = oldStream, oldLife, oldSink
	}()
	cases := []struct {
		name        string
		params      []wasm.ValType
		body        []byte
		args        []uint64
		want        uint64
		reads, sets int
	}{
		{"overlong-immediate", []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0x80, 0, 0x41, 1, 0x6a, 0x22, 0x80, 0, 0x1a, 0x20, 0, 0x0b}, []uint64{7}, 8, 2, 0},
		{"index128", nil, []byte{1, 0x81, 1, 0x7f, 0x41, 17, 0x21, 0x80, 1, 0x20, 0x80, 0x81, 0, 0x0b}, nil, 17, 1, 0},
		{"retained-old-version", []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x41, 9, 0x21, 0, 0x20, 0, 0x6a, 0x0b}, []uint64{7}, 16, 2, 0},
		{"dead-tee-preserves-stack", []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x41, 3, 0x6a, 0x22, 0, 0x0b}, []uint64{7}, 10, 1, 1},
		{"successive-dead-write", []wasm.ValType{wasm.I32}, []byte{0, 0x41, 1, 0x21, 0, 0x41, 2, 0x22, 0, 0x20, 0, 0x6a, 0x0b}, []uint64{7}, 4, 1, 1},
		{"repeated-read-final-transfer", []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x20, 0, 0x6a, 0x0b}, []uint64{7}, 14, 1, 0},
		{"nil-local-void-return", []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x1a, 0x41, 9, 0x0b}, []uint64{7}, 9, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, tc.body)
			for _, on := range []bool{false, true} {
				streamingLocalLifetimesEnabled = on
				for _, intr := range []bool{false, true} {
					opts := streamNumericControlCompile(t, m, intr)
					cm, e := CompileModuleWith(m, opts)
					if e != nil {
						t.Fatal(e)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					streamNumericControlSelected(t, opts)
					if diagnosticsEnabled && !regallocCheckEnabled {
						p := opts.Stats.Funcs[0].Peephole
						reads, sets := 0, 0
						if on {
							reads, sets = tc.reads, tc.sets
						}
						if p["stream-local-last-read"] != reads || p["stream-local-dead-set"] != sets {
							t.Fatalf("on%v counters%v expected reads%d sets%d", on, p, reads, sets)
						}
					}
					if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
						t.Fatalf("on%v intr%v got%d want%d", on, intr, got, tc.want)
					}
				}
			}
		})
	}
}

func TestStreamLifetimeEagerDeadTraps(t *testing.T) {
	oldStream, oldLife, oldSink := streamingOperandsEnabled, streamingLocalLifetimesEnabled, streamingHomeSinkEnabled
	streamingOperandsEnabled = true
	streamingHomeSinkEnabled = false
	defer func() {
		streamingOperandsEnabled, streamingLocalLifetimesEnabled, streamingHomeSinkEnabled = oldStream, oldLife, oldSink
	}()
	for _, tc := range []struct {
		name string
		body []byte
		args []uint64
		pc   uint32
		trap wr.TrapCode
	}{
		{"division-dead-set", []byte{0, 0x20, 0, 0x20, 1, 0x6d, 0x21, 0, 0x41, 7, 0x0b}, []uint64{9, 0}, 4, wr.TrapDivZero},
		{"load-dead-set", []byte{0, 0x20, 0, 0x28, 2, 0, 0x21, 0, 0x41, 7, 0x0b}, []uint64{65535, 0}, 2, wr.TrapLinMemOutOfBounds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, tc.body)
			for _, on := range []bool{false, true} {
				streamingLocalLifetimesEnabled = on
				for _, intr := range []bool{false, true} {
					opts := streamNumericControlCompile(t, m, intr)
					_, _, e := runMemAmd64WithOptions(t, m, opts, nil, tc.args...)
					streamNumericControlSelected(t, opts)
					var trap *wr.TrapError
					if !errors.As(e, &trap) || trap.Code != tc.trap || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != m.Code[0].LocalDeclBytes+tc.pc {
						t.Fatalf("on%v intr%v wrong trap%v", on, intr, e)
					}
					if diagnosticsEnabled && on && opts.Stats.Funcs[0].Peephole["stream-local-dead-set"] != 1 {
						t.Fatal("dead assignment not selected")
					}
				}
			}
		})
	}
}

func TestStreamLifetimeNilLocalCanonicalization(t *testing.T) {
	s := streamHomeUnitState(0)
	for i, id := range s.locals {
		s.release(id)
		s.locals[i] = 0
	}
	id := s.add(streamNode{kind: shared.ScalarConstant, constant: 17, refs: 1})
	s.stack = append(s.stack, id)
	s.canonicalize(true)
	if s.node(s.stack[0]).kind != shared.ScalarRegister || s.owners[s.node(s.stack[0]).reg] != s.stack[0] {
		t.Fatal("nil local lost result owner")
	}
	if s.node(0).refs != 0 {
		t.Fatal("nil sentinel gained ownership")
	}
}
