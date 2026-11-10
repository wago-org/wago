//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestTailLoopInlineNonzeroFactScope(t *testing.T) {
	if profileEnabled {
		t.Skip("tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	// helper(n,a,b): n==0 returns a. Otherwise an if on b guards one
	// recursive step with n=0 and a=a%b. Inputs and results are independent
	// of native-code choices; overwritten zero divisors must still trap.
	tail := []byte{0x41, 0, 0x20, 1, 0x20, 2, 0x70, 0x20, 2, 0x12, 1}
	for _, tc := range []struct {
		name      string
		write     []byte
		tail      []byte
		thenWrite []byte
		b         uint64
		zeroThen  bool
		join      bool
		trap      bool
		elide     bool
	}{
		{name: "unchanged", b: 6, elide: true},
		{name: "set", write: []byte{0x41, 0, 0x21, 2}, b: 6, trap: true},
		{name: "tee", write: []byte{0x41, 0, 0x22, 2, 0x1a}, b: 6, trap: true},
		{name: "select-set", write: []byte{0x41, 0, 0x20, 2, 0x20, 0, 0x1b, 0x21, 2}, b: 6, trap: true},
		{name: "if-set", write: []byte{0x20, 0, 0x04, 0x7f, 0x20, 2, 0x41, 6, 0x6b, 0x05, 0x20, 2, 0x41, 1, 0x6a, 0x0b, 0x21, 2}, b: 6, trap: true},
		{name: "conditional-write", write: []byte{0x20, 0, 0x04, 0x40, 0x41, 0, 0x21, 2, 0x0b}, b: 6, trap: true},
		{name: "branch-skips-write", write: []byte{0x02, 0x40, 0x20, 0, 0x0d, 0, 0x41, 0, 0x21, 2, 0x0b}, b: 6},
		{name: "zero-edge", b: 0, zeroThen: true, trap: true},
		{name: "after-join-nonzero", b: 6, join: true},
		{name: "after-join-zero", b: 0, join: true, trap: true},
		{name: "other-arm-write", thenWrite: []byte{0x41, 0, 0x21, 2}, b: 6},
		{name: "captured-old-divisor", b: 6, elide: true, tail: []byte{0x41, 0, 0x20, 1, 0x20, 2, 0x41, 0, 0x21, 2, 0x70, 0x20, 2, 0x12, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, facts := range []bool{false, true} {
				body := []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 2, 0x45, 0x04}
				step := append(append([]byte{}, tc.write...), tail...)
				if tc.tail != nil {
					step = tc.tail
				}
				if tc.join {
					body = append(body, 0x40, 0x01, 0x05, 0x01, 0x0b)
					body = append(body, step...)
				} else if tc.zeroThen {
					body = append(body, 0x7f)
					body = append(body, step...)
					body = append(body, 0x05, 0x20, 1, 0x0b)
				} else {
					body = append(body, 0x7f)
					body = append(body, tc.thenWrite...)
					body = append(body, 0x20, 1, 0x05)
					body = append(body, step...)
					body = append(body, 0x0b)
				}
				body = append(body, 0x0b, 0x0b)
				params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
				results := []wasm.ValType{wasm.I32}
				m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}}, funcDef{params, results, body})
				opts := CompileOptions{Optimizations: map[string]bool{"value-facts": facts}}
				var stats ModuleStats
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, 1, 17, tc.b)
				if tc.trap {
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
						t.Fatalf("facts=%t got=%d,%v want division by zero", facts, got, err)
					}
				} else if err != nil || got != 5 {
					t.Fatalf("facts=%t got=%d,%v want 5", facts, got, err)
				}
				if diagnosticsEnabled {
					if stats.Funcs[0].Peephole["inline-tail-loop"] == 0 {
						t.Fatal("test did not exercise tail-loop inlining")
					}
					if got := stats.Funcs[0].Peephole["inline-divisor-known-nonzero"] > 0; got != (tc.elide && facts) {
						t.Fatalf("facts=%t elided=%t want %t", facts, got, tc.elide && facts)
					}
				}
			}
		})
	}
}

func TestTailLoopInlineNonzeroFactsFailClosed(t *testing.T) {
	f := fn{policy: currentCodegenPolicy(), inlineTailLoopFrame: 1,
		localBase: 2, localType: []machineType{mtI32, mtI32, mtI64},
		ctrl: []ctrlFrame{{kind: cfLoop}, {kind: cfIf}}}
	e := elem{st: storage{kind: stLocalReg, typ: mtI64, idx: 2}}
	predicate := f.inlineNonzeroPredicate(&e)
	if predicate == 0 {
		t.Fatal("raw local condition was not recognized")
	}
	f.inlineNonzeroPredicates[0] = predicate
	if !f.inlineLocalNonzero(2) {
		t.Fatal("then edge lost its nonzero condition")
	}
	f.ctrl[1].set(ctrlHasElse, true)
	if f.inlineLocalNonzero(2) {
		t.Fatal("zero else edge inherited then's fact")
	}
	f.ctrl[1].set(ctrlHasElse, false)
	f.inlineWriteVersions[0] = inlineWriteVersionLimit - 1
	f.inlineNonzeroPredicates[0] = f.inlineNonzeroPredicate(&e)
	f.invalidateLocalWriteFacts(2)
	f.invalidateLocalWriteFacts(2)
	if f.inlineWriteVersions[0] != inlineWriteVersionLimit || f.inlineNonzeroPredicate(&e) != 0 || f.inlineLocalNonzero(2) {
		t.Fatal("saturated write version reused a stale fact")
	}
	// A deeper predicate must not overwrite an outer descriptor.
	previous := f.inlineNonzeroPredicates
	f.inlineWriteVersions[0] = 0
	f.ctrl = append(f.ctrl, make([]ctrlFrame, len(f.inlineNonzeroPredicates))...)
	f.setInlineNonzeroPredicate(predicate)
	if f.inlineNonzeroPredicates != previous {
		t.Fatal("deep predicate overwrote an outer descriptor")
	}
	var value elem
	value.st.setGCRoot(true)
	value.st.setEHRoot(true)
	value.setElemKind(ekDeferred)
	value.st.setValueFacts(factNonZero)
	if !value.st.hasGCRoot() || !value.st.hasEHRoot() || value.elemKind() != ekDeferred || value.st.valueFacts() != factNonZero {
		t.Fatal("nonzero fact overlaps operand metadata")
	}
}
