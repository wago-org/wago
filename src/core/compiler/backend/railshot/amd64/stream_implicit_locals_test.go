//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func streamImplicitEnable(t *testing.T) {
	t.Helper()
	a, b, c := streamingImplicitLocalsEnabled, streamingOperandsEnabled, streamingRegisterHomesEnabled
	streamingImplicitLocalsEnabled, streamingOperandsEnabled, streamingRegisterHomesEnabled = true, true, false
	t.Cleanup(func() {
		streamingImplicitLocalsEnabled, streamingOperandsEnabled, streamingRegisterHomesEnabled = a, b, c
	})
}
func streamImplicitState(n int) *streamState {
	f := &fn{a: &encoder.Asm{}, nLocals: n, nLocalSlots: n, localSlot: make([]uint32, n)}
	s := &streamState{target: f, widths: make([]bool, n), regs: []uint8{uint8(RAX), uint8(RCX), uint8(RDX), uint8(R8)}, tempBase: n + 1, nextSlot: n + 1}
	s.add(streamNode{})
	for i := 0; i < n; i++ {
		f.localSlot[i] = uint32(i * 8)
		s.locals = append(s.locals, s.add(streamNode{kind: streamBorrow, slot: int32(i), home: uint16(i + 1), refs: 1}))
	}
	s.initializeImplicitLocals()
	return s
}
func TestStreamImplicitMasks(t *testing.T) {
	var m streamLocalMask
	for _, i := range []int{0, 63, 64, 127, 128, 255} {
		m.put(i)
	}
	if m.count() != 6 || m.rank(64) != 3 || m.rank(254) != 5 || m.rank(255) != 6 {
		t.Fatal("word boundary rank")
	}
	for _, want := range []int{0, 63, 64, 127, 128, 255} {
		if got := m.take(); got != want {
			t.Fatalf("take%d want%d", got, want)
		}
	}
	if m.take() != -1 {
		t.Fatal("empty take")
	}
}
func TestStreamImplicitThreeStatesAndAges(t *testing.T) {
	streamImplicitEnable(t)
	s := streamImplicitState(3)
	retired := s.locals[1]
	s.setLocalBinding(1, 0)
	s.release(retired)
	s.restore(0)
	p := s.implicitLocals
	if p.active.count() != 0 || p.implicit.count() != 2 || p.implicit.has(1) || s.localValue(1) != 0 {
		t.Fatal("retired binding resurrected")
	}
	logical := s.nodes[0].order
	physical := len(s.nodes)
	id2 := s.localValue(2)
	id0 := s.localValue(0)
	if s.node(id2).order != p.ageBase+2 || s.node(id0).order != p.ageBase+1 || s.nodes[0].order != logical {
		t.Fatal("lazy access changed reserved logical ages")
	}
	if s.localValue(2) != id2 || s.node(id2).refs != 1 || p.implicit.count() != 0 || p.active.count() != 2 {
		t.Fatal("lazy access/root ownership")
	}
	if len(s.nodes) > physical+2 {
		t.Fatal("excess node construction")
	}
	if s.node(id2).kind != streamBorrow || s.node(id2).slot != 2 || s.node(id2).home != 3 {
		t.Fatal("canonical borrow wrong")
	}
	s.restore(0)
	before := s.nodes[0].order
	fresh := s.add(streamNode{kind: shared.ScalarConstant, constant: 17, refs: 1})
	s.setLocalBinding(2, fresh)
	if s.nodes[0].order != before+1 || s.localValue(2) != fresh || p.implicit.has(2) {
		t.Fatal("overwrite materialized nonexistent old root")
	}
	s.setLocalBinding(0, 0)
	if s.localValue(0) != 0 || p.implicit.has(0) {
		t.Fatal("retired implicit root restored")
	}
	if s.implicitLocalBytes() != 104 {
		t.Fatalf("unaccounted state bytes%d", s.implicitLocalBytes())
	}
}
func TestStreamImplicitSelectedHomesAndReuse(t *testing.T) {
	streamImplicitEnable(t)
	streamGenerationTestEnable(t)
	s := streamResidencyUnitState()
	s.initializeImplicitLocals()
	s.restore(0)
	if s.implicitLocals.implicit.count() != 0 || s.implicitLocals.active.count() != 2 {
		t.Fatal("selected homes became implicit")
	}
	for i, id := range s.locals {
		if id == 0 || s.node(id).kind != streamBacking || s.residency.cells[s.residency.homes[i]].refs != 2 {
			t.Fatal("selected backing root lost")
		}
	}
	old := streamingImplicitLocalsEnabled
	streamingImplicitLocalsEnabled = false
	s.initializeImplicitLocals()
	streamingImplicitLocalsEnabled = old
	if s.implicitLocals != nil || s.Discarded < 104 {
		t.Fatal("reset/accounting")
	}
}
func streamImplicitRuntimeCases() []streamingControlCase {
	p := []wasm.ValType{wasm.I32, wasm.I32}
	cases := []streamingControlCase{
		{"join-implicit-overwrite", p, []byte{0, 0x02, 0x40, 0x0b, 0x41, 9, 0x21, 0, 0x20, 0, 0x20, 1, 0x6a, 0x0b}, []uint64{5, 7}, 16},
		{"join-lazy-direct-return", p, []byte{0, 0x02, 0x40, 0x0b, 0x20, 0, 0x0f, 0x0b}, []uint64{0xdeadbeef00000005, 7}, 5},
		{"detached-stack-old-local", p, []byte{0, 0x20, 0, 0x02, 0x40, 0x41, 9, 0x21, 0, 0x0b, 0x20, 0, 0x6a, 0x0b}, []uint64{5, 7}, 14},
		{"deferred-old-local", p, []byte{0, 0x20, 0, 0x41, 3, 0x6c, 0x02, 0x40, 0x41, 9, 0x21, 0, 0x0b, 0x20, 0, 0x6a, 0x0b}, []uint64{5, 7}, 24},
		{"detached-condition-old-local", p, []byte{0, 0x20, 0, 0x20, 1, 0x49, 0x41, 0xe3, 0, 0x21, 0, 0x04, 0x7f, 0x20, 0, 0x05, 0x20, 1, 0x0b, 0x0b}, []uint64{5, 7}, 99},
	}
	return append(cases, streamingControlCases()...)
}
func TestStreamImplicitRuntimeSnapshots(t *testing.T) {
	streamImplicitEnable(t)
	for _, tc := range streamImplicitRuntimeCases() {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, tc.body)
			for _, on := range []bool{false, true} {
				streamingImplicitLocalsEnabled = on
				for _, intr := range []bool{false, true} {
					opts := streamNumericControlCompile(t, m, intr)
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
						t.Fatalf("on%v intr%v got%d want%d", on, intr, got, tc.want)
					}
					streamNumericControlSelected(t, opts)
					if on && diagnosticsEnabled && !regallocCheckEnabled && (tc.name == "join-lazy-direct-return" || tc.name == "loop-local-joins") {
						p := opts.Stats.Funcs[0].Peephole
						if p["stream-implicit-restore"] == 0 || p["stream-implicit-local-get"] == 0 {
							t.Fatal("implicit path not selected", p)
						}
					}
				}
			}
		})
	}
}
func TestStreamImplicitCallAndTrapBarriers(t *testing.T) {
	streamImplicitEnable(t)
	TestStreamCallLegacyMemoryGrow(t)
	TestStreamNumericControlDroppedTrapOrder(t)
	TestStreamMemoryDroppedLoadTrapPC(t)
}
func TestStreamImplicitAlternateAndWideAges(t *testing.T) {
	streamImplicitEnable(t)
	s := streamImplicitState(256)
	old := s.locals[17]
	s.setLocalBinding(17, 0)
	s.release(old)
	s.restore(0)
	if s.implicitLocals.implicit.has(17) {
		t.Fatal("same-path resurrected retired local")
	}
	s.streamRestore(streamFlow{}, false)
	if !s.implicitLocals.implicit.has(17) || s.implicitLocals.implicit.count() != 256 {
		t.Fatal("alternate control did not restore validated local set")
	}
	base, logical := s.implicitLocals.ageBase, s.nodes[0].order
	for _, i := range []int{255, 127, 64, 63, 17, 0} {
		id := s.localValue(i)
		if s.node(id).order != base+uint16(i+1) || s.nodes[0].order != logical {
			t.Fatalf("lazy age local%d", i)
		}
	}
	for i, id := range s.locals {
		p := s.implicitLocals
		if p.active.has(i) != (id != 0) || p.active.has(i) && p.implicit.has(i) {
			t.Fatal("root state overlap", i)
		}
	}
	s.initializeImplicitLocals()
	if s.implicitLocals.implicit.count() != 0 || s.implicitLocals.ageMask.count() != 0 {
		t.Fatal("pooled initialization retained masks")
	}
}
