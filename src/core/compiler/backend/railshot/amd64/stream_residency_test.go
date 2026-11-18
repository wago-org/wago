//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	wr "github.com/wago-org/wago/src/core/runtime"
	"reflect"
	"testing"
)

func streamResidencyEnable(t *testing.T) {
	t.Helper()
	a, b, c, d := streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled, streamingResidencyEnabled
	streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = true, true, true
	t.Cleanup(func() {
		streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled, streamingResidencyEnabled = a, b, c, d
	})
}
func streamResidencySelected(t *testing.T, opts CompileOptions, on, evac bool) {
	t.Helper()
	streamNumericControlSelected(t, opts)
	if diagnosticsEnabled && !regallocCheckEnabled {
		p := opts.Stats.Funcs[0].Peephole
		if on && p["stream-residency-home"] == 0 || !on && p["stream-residency-home"] != 0 {
			t.Fatalf("residency%v counters%v", on, p)
		}
		if on && evac && p["stream-residency-evacuate"] == 0 {
			t.Fatal("pressure did not exercise home evacuation", p)
		}
	}
}
func TestStreamResidencyRuntimeSnapshots(t *testing.T) {
	streamResidencyEnable(t)
	cases := []streamingControlCase{
		{"two-maps-copy-then-overwrite", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x03, 0x7f, 0x20, 1, 0x21, 0, 0x20, 0, 0x41, 9, 0x21, 1, 0x20, 1, 0x6a, 0x0b, 0x0b}, []uint64{7, 3}, 12},
		{"detached-comparison-old-value", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x03, 0x7f, 0x20, 0, 0x20, 1, 0x49, 0x20, 0, 0x41, 2, 0x6a, 0x21, 0, 0x04, 0x7f, 0x20, 0, 0x05, 0x20, 1, 0x0b, 0x0b, 0x0b}, []uint64{10, 11}, 12},
		{"dirty-i32-home", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x03, 0x7f, 0x20, 0, 0x20, 1, 0x49, 0x0b, 0x0b}, []uint64{0xffffffff00000005, 0x9876543200000009}, 1},
	}
	for _, tc := range streamLocalHomeCases() {
		if tc.name == "home-two-local-backedge-cycle" || tc.name == "home-tee-retained-alias" {
			base := mod1(t, tc.params, []wasm.ValType{wasm.I32}, tc.body)
			n := int(base.Code[0].LocalDeclBytes)
			body := append([]byte{}, tc.body[:n]...)
			body = append(body, 0x03, 0x7f)
			body = append(body, tc.body[n:len(tc.body)-1]...)
			body = append(body, 0x0b, 0x0b)
			tc.body = body
			cases = append(cases, tc)
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, tc.body)
			for _, on := range []bool{false, true} {
				streamingResidencyEnabled = on
				for _, intr := range []bool{false, true} {
					opts := streamNumericControlCompile(t, m, intr)
					cm, e := CompileModuleWith(m, opts)
					if e != nil {
						t.Fatal(e)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					streamResidencySelected(t, opts, on, false)
					if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
						t.Fatalf("on%v intr%v got%d want%d", on, intr, got, tc.want)
					}
				}
			}
		})
	}
}
func TestStreamResidencyPressure(t *testing.T) {
	streamResidencyEnable(t)
	for _, division := range []bool{false, true} {
		body := []byte{1, 12, 0x7f, 0x03, 0x7f}
		for i := 2; i < 14; i++ {
			body = append(body, 0x20, 0, 0x20, 1, 0x6a, 0x41, byte(i), 0x6a, 0x22, byte(i))
		}
		want := uint64(234)
		if division {
			body = append(body, 0x20, 0, 0x20, 1, 0x6e)
			want += 3
		}
		adds := 11
		if division {
			adds++
		}
		for i := 0; i < adds; i++ {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0b, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, on := range []bool{false, true} {
			streamingResidencyEnabled = on
			for _, intr := range []bool{false, true} {
				opts := streamNumericControlCompile(t, m, intr)
				cm, e := CompileModuleWith(m, opts)
				if e != nil {
					t.Fatal(e)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				streamResidencySelected(t, opts, on, true)
				if got := runCompiledAmd64u(t, cm, 9, 3); got != want {
					t.Fatalf("division%v on%v intr%v got%d want%d", division, on, intr, got, want)
				}
			}
		}
	}
}
func TestStreamResidencyDroppedLoadTrap(t *testing.T) {
	streamResidencyEnable(t)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x03, 0x7f, 0x20, 0, 0x28, 2, 0, 0x1a, 0x20, 1, 0x0b, 0x0b})
	for _, on := range []bool{false, true} {
		streamingResidencyEnabled = on
		for _, intr := range []bool{false, true} {
			opts := streamNumericControlCompile(t, m, intr)
			_, _, e := runMemAmd64WithOptions(t, m, opts, nil, 65535, 7)
			streamResidencySelected(t, opts, on, false)
			var trap *wr.TrapError
			if !errors.As(e, &trap) || trap.Code != wr.TrapLinMemOutOfBounds || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != m.Code[0].LocalDeclBytes+4 {
				t.Fatalf("on%v intr%v trap%v", on, intr, e)
			}
		}
	}
}

func streamResidencyUnitState() *streamState {
	f := &fn{a: &encoder.Asm{}, localSlot: []uint32{0, 8}, nLocals: 2, nLocalSlots: 2}
	s := &streamState{target: f, widths: []bool{false, false}, regs: []uint8{uint8(RAX), uint8(RCX), uint8(R12), uint8(R13)}, tempBase: 3, nextSlot: 3, residency: &streamResidency{cells: []streamBackingCell{{}}}}
	s.add(streamNode{})
	for i := 0; i < 2; i++ {
		s.locals = append(s.locals, s.add(streamNode{kind: streamBorrow, slot: int32(i), home: uint16(i + 1), refs: 1}))
	}
	s.initializeRegisterHomes([2]uint16{1, 2}, 2)
	return s
}
func TestStreamResidencyDetachedCellAlias(t *testing.T) {
	oldFlag := streamingHomeSinkEnabled
	streamingHomeSinkEnabled = true
	defer func() { streamingHomeSinkEnabled = oldFlag }()
	s := streamResidencyUnitState()
	cell := s.residency.homes[0]
	old := s.locals[0]
	detached := s.borrowBacking(cell, 0)
	s.retain(old)
	rhs := s.add(streamNode{kind: shared.ScalarConstant, constant: 2, refs: 1})
	expr := s.add(streamNode{kind: streamDeferred, op: shared.IntAdd, left: old, right: rhs, refs: 1})
	if s.node(old).refs != 2 || s.residency.cells[cell].refs != 3 {
		t.Fatal("fixture must distinguish node and backing aliases")
	}
	nodes := append([]streamNode{}, s.nodes...)
	cells := append([]streamBackingCell{}, s.residency.cells...)
	code := append([]byte{}, s.target.a.B...)
	if s.tryHomeSet(0, expr, old) {
		t.Fatal("shared backing must reject in-place overwrite")
	}
	if !reflect.DeepEqual(nodes, s.nodes) || !reflect.DeepEqual(cells, s.residency.cells) || !bytes.Equal(code, s.target.a.B) {
		t.Fatal("rejected cell-alias sink mutated state")
	}
	s.evacuateBacking(cell)
	if a, b := s.operand(old), s.operand(detached); a.Kind != shared.ScalarFrame || b.Kind != shared.ScalarFrame || a.Offset != b.Offset {
		t.Fatal("detached and current node lost shared snapshot")
	}
}
func TestStreamResidencyFrameCellLifetime(t *testing.T) {
	s := streamResidencyUnitState()
	cell := s.residency.homes[0]
	original := s.locals[0]
	alias := s.borrowBacking(cell, 0)
	s.evacuateBacking(cell)
	slot := int(s.residency.cells[cell].slot)
	s.release(original)
	s.locals[0] = 0
	if len(s.freeSlots) != 0 || s.residency.cells[cell].refs != 2 {
		t.Fatal("one node release prematurely freed shared spill")
	}
	s.releaseBacking(cell)
	s.residency.homes[0] = 0
	if len(s.freeSlots) != 0 || s.residency.cells[cell].refs != 1 {
		t.Fatal("map release freed detached snapshot")
	}
	other := s.newBacking(uint8(R10), false)
	if other == cell {
		t.Fatal("reused cell ID while detached node lives")
	}
	s.releaseBacking(other)
	s.release(alias)
	if len(s.freeSlots) != 1 || s.freeSlots[0] != slot {
		t.Fatal("last backing reference must free spill exactly once")
	}
	reused := s.newBacking(uint8(R10), false)
	if reused != cell {
		t.Fatal("expected safe freed-cell reuse")
	}
	if s.slot() != slot || len(s.freeSlots) != 0 {
		t.Fatal("freed spill slot not reusable exactly once")
	}
	if next := s.slot(); next == slot {
		t.Fatal("duplicate spill slot release")
	}
}
func TestStreamResidencyMaterializeRereadsCell(t *testing.T) {
	s := streamResidencyUnitState()
	id := s.locals[0]
	cell := s.residency.homes[0]
	home := s.homeRegs[0]
	s.regs = []uint8{home}
	before := s.target.a.Len()
	slot := s.nextSlot
	got := s.materialize(id, 0)
	var want encoder.Asm
	want.Store32(RSP, s.target.SpillOffset(slot), Reg(home))
	want.Load32(Reg(home), RSP, s.target.SpillOffset(slot))
	if got != home || !bytes.Equal(s.target.a.B[before:], want.B) {
		t.Fatalf("allocation must evacuate then reread backing: got%x want%x", s.target.a.B[before:], want.B)
	}
	if s.node(id).kind != shared.ScalarRegister || s.owners[home] != id || s.residency.registers[home] != 0 || s.residency.cells[cell].kind != shared.ScalarFrame || s.residency.cells[cell].refs != 1 {
		t.Fatal("evacuated home and ordinary owner overlap")
	}
}
func TestStreamResidencyParallelHomeCommit(t *testing.T) {
	s := streamResidencyUnitState()
	oldCells := s.residency.homes
	s.locals[0], s.locals[1] = s.locals[1], s.locals[0]
	s.regs = []uint8{s.homeRegs[0], s.homeRegs[1]}
	before := s.target.a.Len()
	first := s.nextSlot
	s.commitBackingHomes()
	var want encoder.Asm
	want.Store32(RSP, s.target.SpillOffset(first), Reg(s.homeRegs[0]))
	want.Store32(RSP, s.target.SpillOffset(first+1), Reg(s.homeRegs[1]))
	want.Load32(Reg(s.homeRegs[0]), RSP, s.target.SpillOffset(first+1))
	want.Load32(Reg(s.homeRegs[1]), RSP, s.target.SpillOffset(first))
	if !bytes.Equal(s.target.a.B[before:], want.B) {
		t.Fatalf("swap must capture both generations before writes: got%x want%x", s.target.a.B[before:], want.B)
	}
	for _, cell := range oldCells {
		if s.residency.cells[cell].refs != 1 || s.residency.cells[cell].kind != shared.ScalarFrame {
			t.Fatal("old snapshot not retained after map exchange")
		}
	}
	s.restore(0)
	for _, cell := range oldCells {
		if s.residency.cells[cell].refs != 0 {
			t.Fatal("old generation leaked after restore")
		}
	}
	if len(s.freeSlots) != 2 {
		t.Fatal("restore must release both old snapshots once")
	}
}

func TestStreamResidencyUniqueSink(t *testing.T) {
	oldFlag := streamingHomeSinkEnabled
	streamingHomeSinkEnabled = true
	defer func() { streamingHomeSinkEnabled = oldFlag }()
	s := streamResidencyUnitState()
	cell := s.residency.homes[0]
	old := s.locals[0]
	s.retain(old)
	right := s.add(streamNode{kind: shared.ScalarConstant, constant: 2, refs: 1})
	id := s.add(streamNode{kind: streamDeferred, op: shared.IntAdd, left: old, right: right, refs: 1})
	if s.node(old).refs != 2 || s.residency.cells[cell].refs != 2 {
		t.Fatal("unique sink fixture")
	}
	if !s.tryHomeSet(0, id, old) {
		t.Fatal("unique map+node backing should permit sink")
	}
	if s.locals[0] != id || s.node(id).kind != streamBacking || uint32(s.node(id).slot) != cell || s.node(id).refs != 1 || s.residency.cells[cell].refs != 2 || s.node(old).refs != 0 {
		t.Fatal("sink did not transfer backing exactly once")
	}
}
func TestStreamResidencyRepeatedVoidCleanup(t *testing.T) {
	oldRes, oldSink := streamingResidencyEnabled, streamingHomeSinkEnabled
	streamingResidencyEnabled, streamingHomeSinkEnabled = true, true
	defer func() { streamingResidencyEnabled, streamingHomeSinkEnabled = oldRes, oldSink }()
	var s streamState
	var first uint64
	for i := 0; i < 12; i++ {
		f := &fn{a: &encoder.Asm{}, localSlot: []uint32{0, 8}, nLocals: 2, nLocalSlots: 2}
		summary := streamSummary{Eligible: true, Nodes: 32, HasIf: true, HomeLocals: [2]uint16{1, 2}}
		if _, e := s.CompileStream([]byte{0x01, 0x0b}, summary, []bool{false, false}, 2, f); e != nil {
			t.Fatal(e)
		}
		if s.target != nil || s.regs != nil || len(s.residency.cells) != 3 || s.residency.cells[1].refs != 2 || s.residency.cells[2].refs != 2 {
			t.Fatal("void-root state not reset predictably")
		}
		if i == 0 {
			first = s.Memory()
		} else if s.Memory() != first {
			t.Fatal("repeated compile grows retained cell storage")
		}
	}
	s.FinishWorker()
	if s.residency != nil || s.Memory() != 0 {
		t.Fatal("worker finish retains residency storage")
	}
}

func TestStreamResidencyCanonicalResultExcludesHomes(t *testing.T) {
	s := streamResidencyUnitState()
	s.regs = []uint8{s.homeRegs[0], uint8(RAX), s.homeRegs[1], uint8(RCX)}
	id := s.add(streamNode{kind: shared.ScalarConstant, constant: 23, refs: 1})
	s.stack = append(s.stack, id)
	s.canonicalize(true)
	result := s.node(s.stack[0])
	if result.kind != shared.ScalarRegister || s.backingHomeMask()&(1<<result.reg) != 0 || s.owners[result.reg] != s.stack[0] {
		t.Fatal("canonical result clobbers a shared home")
	}
	for reg, cell := range s.residency.registers {
		if cell != 0 && s.owners[reg] != 0 {
			t.Fatal("ordinary owner overlaps shared backing")
		}
	}
}
func TestStreamResidencyDeadPathRestore(t *testing.T) {
	streamResidencyEnable(t)
	body := []byte{1, 12, 0x7f, 0x03, 0x7f, 0x20, 2, 0x04, 0x7f}
	for i := 3; i < 15; i++ {
		body = append(body, 0x20, 0, 0x20, 1, 0x6a, 0x41, byte(i), 0x6a, 0x22, byte(i))
	}
	for i := 0; i < 11; i++ {
		body = append(body, 0x6a)
	}
	body = append(body, 0x0f, 0x05, 0x20, 0, 0x20, 1, 0x6a, 0x0b, 0x0b, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		streamingResidencyEnabled = on
		for _, intr := range []bool{false, true} {
			opts := streamNumericControlCompile(t, m, intr)
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			streamResidencySelected(t, opts, on, true)
			for _, tc := range []struct{ condition, want uint64 }{{0, 10}, {1, 222}} {
				if got := runCompiledAmd64u(t, cm, 7, 3, tc.condition); got != tc.want {
					t.Fatalf("on%v intr%v condition%d got%d want%d", on, intr, tc.condition, got, tc.want)
				}
			}
		}
	}
}

func TestStreamResidencyMemoryPressure(t *testing.T) {
	streamResidencyEnable(t)
	body := []byte{1, 12, 0x7f, 0x03, 0x7f}
	for i := 2; i < 14; i++ {
		body = append(body, 0x20, 0, 0x20, 1, 0x6a, 0x41, byte(i), 0x6a, 0x22, byte(i))
	}
	body = append(body, 0x20, 0, 0x28, 2, 0)
	for i := 0; i < 12; i++ {
		body = append(body, 0x6a)
	}
	body = append(body, 0x0b, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		streamingResidencyEnabled = on
		for _, intr := range []bool{false, true} {
			opts := streamNumericControlCompile(t, m, intr)
			got, _, e := runMemAmd64WithOptions(t, m, opts, func(mem []byte) { mem[9] = 99 }, 9, 3)
			streamResidencySelected(t, opts, on, true)
			if e != nil || got != 333 {
				t.Fatalf("on%v intr%v got%d err%v want333", on, intr, got, e)
			}
		}
	}
}
