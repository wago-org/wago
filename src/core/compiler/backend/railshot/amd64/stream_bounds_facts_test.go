//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestStreamBoundsFactsIdentity(t *testing.T) {
	old := streamingBoundsFactsEnabled
	streamingBoundsFactsEnabled = true
	t.Cleanup(func() { streamingBoundsFactsEnabled = old })
	s := streamState{nodes: []streamNode{{}}, bounds: &streamBoundsCache{}}
	id := s.add(streamNode{kind: shared.ScalarConstant, constant: 5, refs: 1})
	order := s.node(id).order
	s.rememberBounds(id, order, 8)
	if s.node(id).refs != 1 || !s.boundsCover(id, 8) || !s.boundsCover(id, 1) || s.boundsCover(id, 9) {
		t.Fatal("invalid extent/refcount proof")
	}
	s.retain(id)
	s.release(id)
	if !s.boundsCover(id, 8) {
		t.Fatal("unchanged alias lost proof")
	}
	s.release(id)
	if s.boundsCover(id, 1) {
		t.Fatal("dead identity retained proof")
	}
	other := s.add(streamNode{kind: shared.ScalarConstant, constant: 5, refs: 1})
	if other != id || s.node(other).order == order {
		t.Fatal("fixture did not recycle identity with new generation")
	}
	if s.boundsCover(other, 1) {
		t.Fatal("recycled identity inherited proof")
	}
	s.rememberBounds(other, order, 8)
	if s.boundsCover(other, 1) {
		t.Fatal("stale generation record accepted")
	}
	s.rememberBounds(other, s.node(other).order, 8)
	s.clearBoundsFacts()
	if s.boundsCover(other, 1) {
		t.Fatal("barrier retained proof")
	}
	// Repeating the same numeric value in a fresh ID is not an alias proof.
	s.rememberBounds(other, s.node(other).order, 8)
	fresh := s.add(streamNode{kind: shared.ScalarConstant, constant: 5, refs: 1})
	if s.boundsCover(fresh, 1) {
		t.Fatal("fresh equal value inherited identity proof")
	}
}

func TestStreamBoundsFactsSpill(t *testing.T) {
	old := streamingBoundsFactsEnabled
	streamingBoundsFactsEnabled = true
	t.Cleanup(func() { streamingBoundsFactsEnabled = old })
	s := streamState{target: &fn{a: &encoder.Asm{}}, nodes: []streamNode{{}}, bounds: &streamBoundsCache{}, regs: []uint8{uint8(R8)}, tempBase: 1, nextSlot: 1}
	id := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(R8), refs: 1})
	s.owners[R8] = id
	s.rememberBounds(id, s.node(id).order, 17)
	s.spill(id)
	if !s.boundsCover(id, 17) {
		t.Fatal("physical spill changed immutable address identity")
	}
	s.materialize(id, 0)
	if !s.boundsCover(id, 17) {
		t.Fatal("physical reload changed immutable address identity")
	}
}

func TestStreamBoundsFactsRuntime(t *testing.T) {
	oldFacts, oldStream := streamingBoundsFactsEnabled, streamingOperandsEnabled
	t.Cleanup(func() { streamingBoundsFactsEnabled, streamingOperandsEnabled = oldFacts, oldStream })
	streamingOperandsEnabled = true
	cases := []struct {
		name   string
		body   []byte
		args   []uint64
		trap   bool
		fact   bool
		want   uint64
		stored byte
	}{
		{"smaller-extent", []byte{0, 0x20, 0, 0x28, 0, 3, 0x1a, 0x20, 0, 0x2d, 0, 4, 0xb}, []uint64{0}, false, true, 0x5a, 0x5a},
		{"dirty-i32-alias", []byte{0, 0x20, 0, 0x2d, 0, 0, 0x1a, 0x20, 0, 0x2d, 0, 0, 0xb}, []uint64{0xffffeeee00000000}, false, true, 0x5a, 0x5a},
		{"new-local-value", []byte{0, 0x20, 0, 0x2d, 0, 0, 0x1a, 0x41, 0x80, 0x80, 4, 0x21, 0, 0x20, 0, 0x2d, 0, 0, 0xb}, []uint64{0}, true, false, 0, 0x5a},
		{"larger-access-after-store", []byte{0, 0x20, 0, 0x2d, 0, 0, 0x1a, 0x41, 0, 0x41, 0x2a, 0x3a, 0, 0, 0x20, 0, 0x2d, 0, 0x80, 0x80, 4, 0xb}, []uint64{0}, true, false, 0, 0x2a},
		{"control-barrier", []byte{0, 0x20, 0, 0x2d, 0, 0, 0x1a, 0x02, 0x40, 0x0b, 0x20, 0, 0x2d, 0, 0, 0xb}, []uint64{0}, false, false, 0x5a, 0x5a},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var baseline []byte
			for _, on := range []bool{false, true} {
				streamingBoundsFactsEnabled = on
				m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, tc.body)
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, mem, err := runMemAmd64WithOptions(t, m, opts, func(mem []byte) {
					for i := 0; i < 16; i++ {
						mem[i] = 0x5a
					}
				}, tc.args...)
				if (err != nil) != tc.trap || !tc.trap && got != tc.want || mem[0] != tc.stored {
					t.Fatalf("on=%v got=%x stored=%x err=%v", on, got, mem[0], err)
				}
				if !on {
					baseline = mem
				} else if !bytes.Equal(baseline, mem) {
					t.Fatal("bounds facts changed memory effects")
				}
				if diagnosticsEnabled && !regallocCheckEnabled {
					count := stats.Funcs[0].Peephole["stream-bounds-fact"]
					if on && tc.fact && count == 0 || (!on || !tc.fact) && count != 0 {
						t.Fatalf("on=%v unexpected actual-elision count %d", on, count)
					}
				}
			}
		})
	}
}

func TestStreamBoundsFactsCallBarrier(t *testing.T) {
	oldFacts, oldStream, oldJoint, oldInline := streamingBoundsFactsEnabled, streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled
	t.Cleanup(func() {
		streamingBoundsFactsEnabled, streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled = oldFacts, oldStream, oldJoint, oldInline
	})
	streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled = true, true, false
	for _, on := range []bool{false, true} {
		streamingBoundsFactsEnabled = on
		m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x2d, 0, 0, 0x1a, 0x10, 1, 0x1a, 0x20, 0, 0x2d, 0, 0, 0xb}}, funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x41, 0, 0xb}})
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, _, err := runMemAmd64WithOptions(t, m, opts, func(mem []byte) { mem[0] = 0x3a }, 0)
		if err != nil || got != 0x3a {
			t.Fatalf("on=%v result=%x err=%v", on, got, err)
		}
		if diagnosticsEnabled && !regallocCheckEnabled && (stats.Funcs[0].Peephole["streaming-direct-call"] != 1 || stats.Funcs[0].Peephole["stream-bounds-fact"] != 0) {
			t.Fatalf("call-barrier selection mismatch: %v", stats.Funcs[0].Peephole)
		}
	}
}

func TestStreamBoundsFactsTrapAndGrowth(t *testing.T) {
	old := streamingBoundsFactsEnabled
	streamingBoundsFactsEnabled = true
	t.Cleanup(func() { streamingBoundsFactsEnabled = old })
	t.Run("dropped-load-source-pc", TestStreamMemoryDroppedLoadTrapPC)
	t.Run("legacy-growth", TestStreamCallLegacyMemoryGrow)
}
