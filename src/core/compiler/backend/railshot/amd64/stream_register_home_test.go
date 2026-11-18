//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

// Prepared independent fixtures; integration wrapper awaits explicit home-map override.
func streamLocalHomeCases() []streamingControlCase {
	swap := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x45, 0x0d, 1, 0x20, 0, 0x21, 3, 0x20, 1, 0x21, 0, 0x20, 3, 0x21, 1, 0x20, 2, 0x41, 1, 0x6b, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x41, 10, 0x6c, 0x20, 1, 0x6a, 0x0b}
	snap := []byte{0, 0x02, 0x7f, 0x20, 0, 0x41, 9, 0x21, 0, 0x20, 1, 0x0d, 0, 0x41, 1, 0x6a, 0x0b, 0x20, 0, 0x6a, 0x0b}
	join := []byte{0, 0x20, 2, 0x04, 0x7f, 0x20, 0, 0x05, 0x20, 1, 0x0b, 0x41, 2, 0x6c, 0x0b}
	nested := []byte{1, 1, 0x7f, 0x02, 0x7f, 0x03, 0x40, 0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, 0x0c, 2, 0x0b, 0x41, 2, 0x21, 2, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x45, 0x0d, 1, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6b, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x00, 0x0b, 0x0b}
	return []streamingControlCase{
		{"home-two-local-backedge-cycle", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x03, 0x40, 0x20, 0, 0x20, 1, 0x21, 0, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x20, 0, 0x41, 0, 0x4a, 0x0d, 0, 0x0b, 0x20, 0, 0x41, 10, 0x6c, 0x20, 1, 0x6a, 0x0b}, []uint64{3, 2}, 2},
		{"home-tee-retained-alias", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x20, 0, 0x20, 1, 0x22, 0, 0x20, 0, 0x6a, 0x6a, 0x0b}, []uint64{7, 11}, 29},
		{"home-detached-old-condition", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x20, 0, 0x41, 0, 0x21, 0, 0x04, 0x7f, 0x20, 1, 0x05, 0x41, 42, 0x0b, 0x0b}, []uint64{1, 11}, 11},
		{"home-nested-loop-result-branch", []wasm.ValType{wasm.I32, wasm.I32}, nested, []uint64{3, 5}, 11},
		{"home-swap-odd-backedge", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, swap, []uint64{7, 2, 3}, 27},
		{"home-swap-even-backedge", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, swap, []uint64{7, 2, 4}, 72},
		{"home-old-read-taken", []wasm.ValType{wasm.I32, wasm.I32}, snap, []uint64{7, 1}, 16},
		{"home-old-read-fallthrough", []wasm.ValType{wasm.I32, wasm.I32}, snap, []uint64{7, 0}, 17},
		{"home-result-predecessor-left", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, join, []uint64{7, 11, 1}, 14},
		{"home-result-predecessor-right", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, join, []uint64{7, 11, 0}, 22},
		{"home-zero-before-set", []wasm.ValType{wasm.I32}, []byte{1, 1, 0x7f, 0x20, 1, 0x20, 0, 0x21, 1, 0x20, 1, 0x6a, 0x0b}, []uint64{17}, 17},
		{"home-dirty-i32-unsigned", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x20, 0, 0x20, 1, 0x49, 0x0b}, []uint64{0xffffffff00000005, 0x9876543200000009}, 1},
	}
}

func TestStreamRegisterHomeControl(t *testing.T) {
	oldStream, oldHomes := streamingOperandsEnabled, streamingRegisterHomesEnabled
	streamingOperandsEnabled = true
	defer func() { streamingOperandsEnabled = oldStream; streamingRegisterHomesEnabled = oldHomes }()
	for _, tc := range streamLocalHomeCases() {
		t.Run(tc.name, func(t *testing.T) {
			// A single-iteration result loop ensures natural loop-only home selection,
			// without a production override or changing lexical branch depths inside it.
			base := mod1(t, tc.params, []wasm.ValType{wasm.I32}, tc.body)
			n := int(base.Code[0].LocalDeclBytes)
			body := append([]byte{}, tc.body[:n]...)
			body = append(body, 0x03, 0x7f)
			body = append(body, tc.body[n:len(tc.body)-1]...)
			body = append(body, 0x0b, 0x0b)
			m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, body)
			for _, intr := range []bool{false, true} {
				for _, homes := range []bool{false, true} {
					streamingRegisterHomesEnabled = homes
					opts := streamNumericControlCompile(t, m, intr)
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					streamNumericControlSelected(t, opts)
					if diagnosticsEnabled && !regallocCheckEnabled {
						got := opts.Stats.Funcs[0].Peephole["stream-local-home"]
						if homes && got == 0 || !homes && got != 0 {
							t.Fatalf("homes=%v selected=%d", homes, got)
						}
						if homes && tc.name == "home-two-local-backedge-cycle" && got != 2 {
							t.Fatalf("cycle requires both locals selected, got=%d", got)
						}
					}
					if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
						t.Fatalf("homes=%v interruptible=%v got=%d want=%d", homes, intr, got, tc.want)
					}
				}
			}
		})
	}
}

func TestStreamRegisterHomeBulkPoll(t *testing.T) {
	oldStream, oldHomes := streamingOperandsEnabled, streamingRegisterHomesEnabled
	streamingOperandsEnabled, streamingRegisterHomesEnabled = true, true
	defer func() { streamingOperandsEnabled, streamingRegisterHomesEnabled = oldStream, oldHomes }()
	tc := streamLocalHomeCases()[0]
	if tc.name != "home-two-local-backedge-cycle" {
		t.Fatal("cycle fixture order")
	}
	// Fill on every backedge iteration. Both locals are homes; bulk scratch and
	// interrupt poll must preserve their values across the swap and branch.
	body := append([]byte{}, tc.body[:3]...)
	body = append(body, 0x41, 0, 0x41, 7, 0x41, 8, 0xfc, 11, 0)
	body = append(body, tc.body[3:]...)
	m := modMem(t, 1, tc.params, []wasm.ValType{wasm.I32}, body)
	for _, intr := range []bool{false, true} {
		opts := streamNumericControlCompile(t, m, intr)
		got, mem, err := runMemAmd64WithOptions(t, m, opts, nil, tc.args...)
		if err != nil || got != tc.want {
			t.Fatalf("intr=%v got=%d err=%v want=%d", intr, got, err, tc.want)
		}
		streamNumericControlSelected(t, opts)
		if diagnosticsEnabled && !regallocCheckEnabled && opts.Stats.Funcs[0].Peephole["stream-local-home"] != 2 {
			t.Fatal("both homes must be selected across bulk/poll")
		}
		for i := 0; i < 8; i++ {
			if mem[i] != 7 {
				t.Fatalf("fill[%d]=%d", i, mem[i])
			}
		}
	}
}

func streamHomeUnitState(reserved uint64) *streamState {
	f := &fn{a: &encoder.Asm{}, localSlot: []uint32{0, 8}, nLocals: 2, nLocalSlots: 2}
	s := &streamState{target: f, widths: []bool{false, false}, regs: []uint8{uint8(RAX), uint8(RCX), uint8(RDX)}, reserved: reserved, tempBase: 3, nextSlot: 3}
	s.add(streamNode{})
	for i := 0; i < 2; i++ {
		s.locals = append(s.locals, s.add(streamNode{kind: streamBorrow, slot: int32(i), home: uint16(i + 1), refs: 1}))
	}
	s.initializeRegisterHomes([2]uint16{1, 2}, 2)
	return s
}
func TestStreamRegisterHomeSpillBacking(t *testing.T) {
	for _, reserved := range []uint64{0, 1<<R12 | 1<<R13 | 1<<R14} {
		s := streamHomeUnitState(reserved)
		for _, r := range s.homeRegs {
			if (uint64(1)<<r)&(reserved|1<<RAX|1<<RCX|1<<RDX|1<<RSI|1<<RDI|1<<R8) != 0 {
				t.Fatalf("unsafe home register%d", r)
			}
		}
		id := s.locals[0]
		r := s.materialize(id, 0)
		if s.reserved&(1<<r) != 0 || s.owners[r] != id {
			t.Fatal("borrow materialization owns a home/reserved register")
		}
		if s.node(id).home&streamRegisterHome == 0 {
			t.Fatal("lost register-home provenance")
		}
		before := s.target.a.Len()
		slot := s.nextSlot
		var expected encoder.Asm
		expected.Store32(RSP, s.target.SpillOffset(slot), Reg(r))
		s.spill(id)
		if !bytes.Equal(s.target.a.B[before:], expected.B) {
			t.Fatal("tagged home spill must emit real frame store")
		}
		if n := s.node(id); n.kind != shared.ScalarFrame || n.home != 0 || s.owners[r] != 0 || int(n.slot) != slot || s.Spills != 1 {
			t.Fatalf("invalid spill backing %+v owners=%v", *n, s.owners)
		}
	}
}
func TestStreamRegisterHomeResultCommitOrder(t *testing.T) {
	s := streamHomeUnitState(0)
	s.release(s.locals[0])
	s.locals[0] = s.add(streamNode{kind: shared.ScalarConstant, constant: 17, refs: 1})
	localReg := s.materialize(s.locals[0], 0)
	result := s.add(streamNode{kind: shared.ScalarConstant, constant: 23, refs: 1})
	s.stack = append(s.stack, result)
	resultReg := s.materialize(result, 0)
	if localReg != uint8(RAX) || resultReg != uint8(RCX) {
		t.Fatal("fixture allocation order")
	}
	before := s.target.a.Len()
	s.canonicalize(true)
	var expected encoder.Asm
	expected.MovRegReg32(R12, RAX)
	expected.MovRegReg32(RAX, RCX)
	if !bytes.Equal(s.target.a.B[before:], expected.B) {
		t.Fatalf("commit must capture changed home before result overwrites register: got%x want%x", s.target.a.B[before:], expected.B)
	}
	if s.node(s.stack[0]).reg != uint8(RAX) || s.owners[RAX] != s.stack[0] {
		t.Fatal("result ownership lost")
	}
}
