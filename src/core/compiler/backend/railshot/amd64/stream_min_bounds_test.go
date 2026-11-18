//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wr "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestStreamMinimumBoundsQuery(t *testing.T) {
	const unknown = uint64(^uint32(0))
	s := streamState{nodes: []streamNode{{}}}
	constant := func(v int64) streamID { return s.add(streamNode{kind: shared.ScalarConstant, constant: v, refs: 1}) }
	expr := func(op shared.IntOp, l, r streamID) streamID {
		return s.add(streamNode{kind: streamDeferred, op: op, left: l, right: r, refs: 1})
	}
	opaque := s.add(streamNode{kind: shared.ScalarRegister, constant: 12345, refs: 1})
	mask := expr(shared.IntAnd, opaque, constant(255))
	scaled := expr(shared.IntShl, mask, constant(2))
	for _, tc := range []struct {
		id   streamID
		want uint64
	}{
		{constant(-1), unknown}, {opaque, unknown}, {mask, 255}, {scaled, 1020},
		{expr(shared.IntAdd, scaled, constant(4)), 1024},
		{expr(shared.IntAdd, constant(-1), constant(1)), unknown},
		{expr(shared.IntShl, constant(0x80000000), constant(1)), unknown},
		{expr(shared.IntAdd, opaque, mask), unknown},
		{expr(shared.IntShrU, opaque, constant(24)), 255},
		{s.add(streamNode{kind: streamBorrow, slot: 0, refs: 1}), unknown},
		{s.add(streamNode{kind: shared.ScalarConstant, constant: 0, wide: true, refs: 1}), unknown},
	} {
		budget := 32
		if got := s.streamUnsignedAddressMaximum(tc.id, &budget); got != tc.want {
			t.Fatalf("id=%d max=%d want%d", tc.id, got, tc.want)
		}
		if budget < 0 || budget >= 32 {
			t.Fatal("query violated visit budget")
		}
	}
	// A deeply shared DAG is counted once per visited occurrence, not per depth.
	dag := opaque
	for i := 0; i < 20; i++ {
		dag = expr(shared.IntAdd, dag, dag)
	}
	budget := 32
	if got := s.streamUnsignedAddressMaximum(dag, &budget); got != unknown || budget != 0 {
		t.Fatalf("shared DAG max=%d remaining=%d", got, budget)
	}
}

func TestStreamMinimumBoundsRuntime(t *testing.T) {
	oldMin, oldStream, oldFacts := streamingMinimumBoundsEnabled, streamingOperandsEnabled, streamingBoundsFactsEnabled
	t.Cleanup(func() {
		streamingMinimumBoundsEnabled, streamingOperandsEnabled, streamingBoundsFactsEnabled = oldMin, oldStream, oldFacts
	})
	streamingOperandsEnabled = true
	streamingBoundsFactsEnabled = false
	cases := []struct {
		name        string
		body        []byte
		arg         uint64
		trap, proof bool
	}{
		{"constant", []byte{0, 0x41, 17, 0x2d, 0, 0, 0xb}, 0, false, true},
		{"masked-scaled", []byte{0, 0x20, 0, 0x41, 0xff, 1, 0x71, 0x41, 2, 0x74, 0x41, 4, 0x6a, 0x28, 0, 0, 0xb}, 0xffff1234ffffffff, false, true},
		{"unknown-base", []byte{0, 0x20, 0, 0x2d, 0, 0, 0xb}, 65536, true, false},
		{"possible-wrap", []byte{0, 0x20, 0, 0x41, 1, 0x6a, 0x2d, 0, 0, 0xb}, 0xffffffff, false, false},
		{"nonwrapping-memarg", []byte{0, 0x41, 1, 0x2d, 0, 0xff, 0xff, 0xff, 0xff, 15, 0xb}, 0, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, on := range []bool{false, true} {
				streamingMinimumBoundsEnabled = on
				m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, tc.body)
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"memory-min-bounds": true}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, _, err := runMemAmd64WithOptions(t, m, opts, nil, tc.arg)
				if (err != nil) != tc.trap || !tc.trap && got != 0 {
					t.Fatalf("on=%v result=%x err=%v", on, got, err)
				}
				if diagnosticsEnabled && !regallocCheckEnabled {
					hits := stats.Funcs[0].Peephole["stream-min-bounds"]
					if on && tc.proof && hits != 1 || (!on || !tc.proof) && hits != 0 {
						t.Fatalf("on=%v proofcount%d", on, hits)
					}
				}
			}
		})
	}
}

func TestStreamMinimumBoundsUnknownGlobal(t *testing.T) {
	oldMin, oldStream, oldJoint := streamingMinimumBoundsEnabled, streamingOperandsEnabled, streamingCallGlobalsEnabled
	t.Cleanup(func() {
		streamingMinimumBoundsEnabled, streamingOperandsEnabled, streamingCallGlobalsEnabled = oldMin, oldStream, oldJoint
	})
	streamingMinimumBoundsEnabled, streamingOperandsEnabled, streamingCallGlobalsEnabled = true, true, true
	m := streamGlobalFixture(t, false, true, false)
	m.Code[0].BodyBytes = []byte{0x23, 0, 0x2d, 0, 0, 0x0b}
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	var stats ModuleStats
	opts := CompileOptions{Optimizations: map[string]bool{"memory-min-bounds": true}}
	if diagnosticsEnabled {
		opts.Stats = &stats
	}
	_, _, err := runStreamGlobalFixture(t, m, opts, 65536, 0)
	if err == nil {
		t.Fatal("mutable imported-global base escaped bounds check")
	}
	if diagnosticsEnabled && !regallocCheckEnabled && stats.Funcs[0].Peephole["stream-min-bounds"] != 0 {
		t.Fatal("unknown global received minimum proof")
	}
}

// The division is eager even though masking its result proves the subsequent
// load safe. Eliminating a memory check must not remove or move the child trap.
func TestStreamMinimumBoundsEagerChildTrap(t *testing.T) {
	oldMin, oldStream, oldFacts := streamingMinimumBoundsEnabled, streamingOperandsEnabled, streamingBoundsFactsEnabled
	t.Cleanup(func() {
		streamingMinimumBoundsEnabled, streamingOperandsEnabled, streamingBoundsFactsEnabled = oldMin, oldStream, oldFacts
	})
	streamingOperandsEnabled = true
	streamingBoundsFactsEnabled = false
	body := []byte{0, 0x20, 0, 0x20, 1, 0x6e, 0x41, 0xff, 1, 0x71, 0x2d, 0, 0, 0x1a, 0x00, 0x0b}
	for _, on := range []bool{false, true} {
		for _, interruptible := range []bool{false, true} {
			for _, denominator := range []uint64{0, 2} {
				streamingMinimumBoundsEnabled = on
				m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
				var stats ModuleStats
				opts := CompileOptions{Interruptible: interruptible, Optimizations: map[string]bool{"memory-min-bounds": true}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				_, _, err := runMemAmd64WithOptions(t, m, opts, nil, 7, denominator)
				wantCode, wantPC := wr.TrapDivZero, m.Code[0].LocalDeclBytes+4
				if denominator != 0 {
					wantCode, wantPC = wr.TrapUnreachable, m.Code[0].LocalDeclBytes+13
				}
				var trap *wr.TrapError
				if !errors.As(err, &trap) || trap.Code != wantCode || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != wantPC {
					t.Fatalf("on=%v interruptible=%v divisor=%d trap=%+v want=%v/%d", on, interruptible, denominator, trap, wantCode, wantPC)
				}
				if diagnosticsEnabled && !regallocCheckEnabled {
					if stats.Funcs[0].Peephole["streaming-function"] != 1 {
						t.Fatal("eager-child fixture fell back")
					}
					hits := stats.Funcs[0].Peephole["stream-min-bounds"]
					if on && hits != 1 || !on && hits != 0 {
						t.Fatalf("on=%v proofcount=%d", on, hits)
					}
				}
			}
		}
	}
}
