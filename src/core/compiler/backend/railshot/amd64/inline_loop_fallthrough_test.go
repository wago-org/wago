//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestInlineFallthroughPreservesFloatConstantLifetime(t *testing.T) {
	for depth := 0; depth <= 3; depth++ {
		var frame ctrlFrame
		frame.setFloatConstBase(depth)
		if frame.has(ctrlInlineFallthrough) {
			t.Fatal("constant cache depth aliases inline boundary")
		}
		frame.set(ctrlInlineFallthrough, true)
		if frame.floatConstBase() != depth {
			t.Fatal("inline boundary changed constant lifetime")
		}
		frame.setFloatConstBase((depth + 1) % 4)
		if !frame.has(ctrlInlineFallthrough) {
			t.Fatal("constant lifetime cleared inline boundary")
		}
		frame.set(ctrlInlineFallthrough, false)
		if frame.floatConstBase() != (depth+1)%4 {
			t.Fatal("clearing inline boundary changed constant lifetime")
		}
	}
}

func TestTupleLoopInternalBranchesProof(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"internal", []byte{3, 0x40, 0x0c, 0, 0x0b, 0x0b}, true},
		{"conditional-internal", []byte{2, 0x40, 3, 0x40, 0x41, 1, 0x0d, 1, 0x0b, 0x0b, 0x0b}, true},
		{"function-branch", []byte{3, 0x40, 0x0c, 1, 0x0b, 0x0b}, false},
		{"function-conditional", []byte{3, 0x40, 0x41, 1, 0x0d, 1, 0x0b, 0x0b}, false},
		{"return", []byte{3, 0x40, 0x0f, 0x0b, 0x0b}, false},
		{"trap", []byte{3, 0x40, 0, 0x0b, 0x0b}, false},
		{"call", []byte{3, 0x40, 0x10, 0, 0x0b, 0x0b}, false},
		{"memory", []byte{3, 0x40, 0x28, 2, 0, 0x0b, 0x0b}, false},
		{"global", []byte{3, 0x40, 0x23, 0, 0x0b, 0x0b}, false},
		{"float", []byte{3, 0x40, 0x92, 0x0b, 0x0b}, false},
		{"truncated", []byte{3, 0x40, 0x41}, false},
		{"trailing", []byte{3, 0x40, 0x0b, 0x0b, 1}, false},
		{"no-loop", []byte{1, 0x0b}, false},
		{"outer-else", []byte{3, 0x40, 0x0b, 5, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := &inlineTarget{params: inlineTupleLoopFlag, body: tc.body}
			if got := tupleLoopHasOnlyInternalBranches(target, wasm.ModuleInstructionClassifier{}); got != tc.want {
				t.Fatalf("got %t, want %t", got, tc.want)
			}
		})
	}
}

func TestInlineTupleLoopFallthroughNonreturning(t *testing.T) {
	// Internal backedges need not terminate. The unreachable final end must
	// keep the ordinary polymorphic-stack handling rather than assume results.
	m := modFuncs(t,
		funcDef{nil, []wasm.ValType{wasm.I32}, []byte{0, 2, 0x7f, 0x10, 1, 0x1a, 0x0b, 0x0b}},
		funcDef{nil, []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 3, 0x40, 0x0c, 0, 0x0b, 0x0b}})
	for _, workers := range []int{1, 4} {
		compiled, err := CompileModuleWith(m, CompileOptions{Workers: workers, DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		if compiled.CodeImage != nil {
			compiled.CodeImage.Close()
		}
	}
}

func TestInlineTupleLoopFallthroughDroppedResultTrap(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, div, add := wasm.I32, byte(0x41), byte(0x6e), byte(0x6a)
		if wide {
			typ, k, div, add = wasm.I64, 0x42, 0x80, 0x7c
		}
		// A reachable loop followed by a deferred trapping result expression.
		helper := []byte{0, 3, 0x40, 1, 0x0b, 0x20, 0, 0x20, 0, 0x20, 1, div, 0x0b}
		bt := byte(0x7f)
		if wide {
			bt = 0x7e
		}
		caller := []byte{0, k, 7, 2, bt, 0x20, 0, 0x20, 1, 0x10, 1, 0x1a, 0x0b, add, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, divisor := range []uint64{0, 3} {
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"straight-line-block": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, 19, divisor)
					if divisor == 0 {
						var trap *rt.TrapError
						if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
							t.Fatalf("wide=%t enabled=%t: %v", wide, enabled, err)
						}
					} else if err != nil || got != 26 {
						t.Fatalf("got=%d err=%v", got, err)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-loop-fallthrough"] != 0) != (enabled && !profileEnabled) {
						t.Fatalf("trapping-result boundary not exercised: %v", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}

func TestInlineTupleLoopFallthroughLiveResults(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, lt, sub, add, mul := wasm.I32, byte(0x7f), byte(0x41), byte(0x49), byte(0x6b), byte(0x6a), byte(0x6c)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, lt, sub, add, mul = wasm.I64, 0x7e, 0x42, 0x54, 0x7d, 0x7c, 0x7e
			fold = func(x uint64) uint64 { return x }
		}
		// Subtract successive odd integers, returning (root, remainder).
		helper := []byte{1, 2, bt, k, 1, 0x21, 2, 2, 0x40, 3, 0x40,
			0x20, 0, 0x20, 2, lt, 0x0d, 1,
			0x20, 0, 0x20, 2, sub, 0x21, 0,
			0x20, 2, k, 2, add, 0x21, 2,
			0x20, 1, k, 1, add, 0x21, 1,
			0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x20, 0, 0x0b}
		// Both first-call results remain live across the second call, with an
		// older caller operand below them. Private local homes must not alias.
		caller := []byte{0, 2, bt, 2, bt, 0x20, 2, 0x20, 0, 0x10, 1, 0x20, 1, 0x10, 1,
			k, 3, mul, add, add, add, add, 0x0b, 0x0b, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ, typ}, helper})
		for _, mode := range []string{"enabled", "inline-off", "block-off"} {
			for _, workers := range []int{1, 4} {
				for _, input := range [][3]uint64{{0, 1, 7}, {1, 0, ^uint64(0)}, {2, 37, 19}, {64, 13, 0x8000000080000001}, {997, 26, 0xfffffff0}} {
					root := func(x uint64) (uint64, uint64) {
						var n uint64
						for odd := uint64(1); x >= odd; odd += 2 {
							x -= odd
							n++
						}
						return n, x
					}
					a, b := root(input[0])
					c, d := root(input[1])
					want := fold(input[2] + a + b + c + 3*d)
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": mode != "inline-off", "straight-line-block": mode != "block-off", "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input[:]...)
					if err != nil || got != want {
						t.Fatalf("wide=%t mode=%s workers=%d input=%x got=%x err=%v want=%x", wide, mode, workers, input, got, err, want)
					}
					if diagnosticsEnabled {
						wantHits := 0
						if mode == "enabled" && !profileEnabled {
							wantHits = 2
						}
						if got := stats.Funcs[0].Peephole["inline-tuple-loop-fallthrough"]; int(got) != wantHits {
							t.Fatalf("wide=%t mode=%s profile=%t hits=%v want=%d", wide, mode, profileEnabled, stats.Funcs[0].Peephole, wantHits)
						}
						if got := stats.Funcs[0].Peephole["straight-line-block"]; mode == "enabled" && !profileEnabled && got < 2 {
							t.Fatalf("nested spliced caller blocks were not admitted: %v", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
