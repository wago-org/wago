//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func streamRestoreReuseEnable(t *testing.T) {
	old := streamingRestoreReuseEnabled
	streamingRestoreReuseEnabled = true
	t.Cleanup(func() { streamingRestoreReuseEnabled = old })
}
func streamRestoreReuseState() *streamState {
	f := &fn{a: &encoder.Asm{}, localSlot: []uint32{0, 8}, nLocals: 2, nLocalSlots: 2, stats: &CodegenStats{}}
	s := &streamState{target: f, widths: []bool{false, false}, regs: []uint8{uint8(RAX), uint8(RCX), uint8(RDX)}, tempBase: 3, nextSlot: 3}
	s.add(streamNode{})
	for i := 0; i < 2; i++ {
		s.locals = append(s.locals, s.add(streamNode{kind: streamBorrow, slot: int32(i), home: uint16(i + 1), refs: 1}))
	}
	return s
}
func streamRestoreReuseCount(s *streamState) int {
	return s.target.stats.Peephole["stream-restore-local-reuse"]
}
func TestStreamRestoreReuseIdentityAndOrder(t *testing.T) {
	streamRestoreReuseEnable(t)
	s := streamRestoreReuseState()
	ids := append([]streamID{}, s.locals...)
	before := s.nodes[0].order
	s.restore(0)
	for i, id := range ids {
		if s.locals[i] != id || s.node(id).refs != 1 || s.node(id).order != before+uint16(i+1) {
			t.Fatalf("local%d identity/order/ref mismatch: %+v", i, *s.node(s.locals[i]))
		}
	}
	if s.nodes[0].order != before+2 {
		t.Fatal("retention bypassed creation budget")
	}
	if diagnosticsEnabled && streamRestoreReuseCount(s) != 2 {
		t.Fatal("reuse did not activate", streamRestoreReuseCount(s))
	}
}
func TestStreamRestoreReuseRejectsAliases(t *testing.T) {
	streamRestoreReuseEnable(t)
	for _, kind := range []string{"stack", "detached", "two-locals", "deferred-child", "wrong-home", "wrong-slot", "retired"} {
		t.Run(kind, func(t *testing.T) {
			s := streamRestoreReuseState()
			id := s.locals[0]
			var detached streamID
			switch kind {
			case "stack":
				s.retain(id)
				s.stack = append(s.stack, id)
			case "detached":
				s.retain(id)
				detached = id
			case "two-locals":
				s.release(s.locals[1])
				s.locals[1] = id
				s.retain(id)
			case "deferred-child":
				s.retain(id)
				rhs := s.add(streamNode{kind: shared.ScalarConstant, constant: 2, refs: 1})
				detached = s.add(streamNode{kind: streamDeferred, op: shared.IntAdd, left: id, right: rhs, refs: 1})
			case "wrong-home":
				s.node(id).home = 0
			case "wrong-slot":
				s.node(id).slot = 1
			case "retired":
				s.release(id)
				s.locals[0] = 0
			}
			s.restore(len(s.stack))
			want := 1
			if kind == "two-locals" {
				want = 0
			}
			if diagnosticsEnabled && streamRestoreReuseCount(s) != want {
				t.Fatalf("reuse%d want%d", streamRestoreReuseCount(s), want)
			}
			if kind == "retired" && s.locals[0] != 0 {
				t.Fatal("resurrected dead local")
			}
			if detached != 0 {
				s.release(detached)
			}
		})
	}
}
func TestStreamRestoreReuseExcludesHomesAndAlternatePath(t *testing.T) {
	streamRestoreReuseEnable(t)
	streamResidencyEnable(t)
	streamingResidencyEnabled = true
	s := streamResidencyUnitState()
	s.target.stats = &CodegenStats{}
	s.restore(0)
	if streamRestoreReuseCount(s) != 0 {
		t.Fatal("backed homes reused as ordinary borrows")
	}
	s = streamRestoreReuseState()
	before := s.nodes[0].order
	s.streamRestore(streamFlow{}, false)
	if streamRestoreReuseCount(s) != 0 || s.nodes[0].order != before+2 {
		t.Fatal("alternate-path restore retained bindings or failed rebuilding")
	}
}
func TestStreamRestoreReuseStackRecycling(t *testing.T) {
	streamRestoreReuseEnable(t)
	s := streamRestoreReuseState()
	keep := s.locals[0]
	oldOrder := s.nodes[0].order
	s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarConstant, constant: 5, refs: 1}))
	s.restore(1)
	if s.locals[0] != keep || s.node(keep).kind != streamBorrow || s.stack[0] == keep || s.node(keep).order <= oldOrder {
		t.Fatal("stack reconstruction recycled retained binding")
	}
	if s.node(s.stack[0]).kind != shared.ScalarFrame {
		t.Fatal("stack not reconstructed")
	}
}
func TestStreamRestoreReuseRuntimeNativeParity(t *testing.T) {
	streamRestoreReuseEnable(t)
	old := streamingOperandsEnabled
	streamingOperandsEnabled = true
	defer func() { streamingOperandsEnabled = old }()
	total := 0
	for _, tc := range streamLocalHomeCases() {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.params, []wasm.ValType{wasm.I32}, tc.body)
			for _, intr := range []bool{false, true} {
				var previous []byte
				for _, on := range []bool{false, true} {
					streamingRestoreReuseEnabled = on
					opts := streamNumericControlCompile(t, m, intr)
					cm, e := CompileModuleWith(m, opts)
					if e != nil {
						t.Fatal(e)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					streamNumericControlSelected(t, opts)
					if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
						t.Fatalf("on%v intr%v got%d want%d", on, intr, got, tc.want)
					}
					if !on {
						previous = append([]byte{}, cm.Code...)
					} else {
						if !bytes.Equal(previous, cm.Code) {
							t.Fatal("metadata retention changed native image")
						}
						if diagnosticsEnabled {
							total += opts.Stats.Funcs[0].Peephole["stream-restore-local-reuse"]
						}
					}
				}
			}
		})
	}
	if diagnosticsEnabled && !regallocCheckEnabled && total == 0 {
		t.Fatal("native parity only tested fallback")
	}
}
