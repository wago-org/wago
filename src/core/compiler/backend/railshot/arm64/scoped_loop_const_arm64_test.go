//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestScopedLoopConstLeaseRestoresOuterCache(t *testing.T) {
	h := funcHintView{loopIntConstCount: 2, loopIntConstTypes: 5, loopIntConst: [4]int64{0x1234567, 0x2345678}}
	f := fn{a: &a64.Asm{}, makesCalls: true, scopedConstHints: scopedConstHintsFrom(&h), policy: currentCodegenPolicy()}
	f.intervalControl = true
	f.pinnedLocalMask = maskOf(X19).union(maskOf(X20)).union(maskOf(X21)).union(maskOf(X22))
	originalLimit := intervalRegionRegLimit(f.reserved.union(f.pinnedLocalMask))
	original := f.reserved
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 0 {
		t.Fatal("function-wide cache admitted across calls")
	}
	outer := ctrlFrame{kind: cfLoop, flags: ctrlLoopCallFree | 1<<12}
	f.preloadScopedLoopConsts(&outer)
	if !outer.has(ctrlLoopConstScope) || f.iconstN != 1 {
		t.Fatal("missing outer lease")
	}
	outerReg := f.iconsts[0].reg
	inner := ctrlFrame{kind: cfLoop, flags: ctrlLoopCallFree | 2<<12}
	f.preloadScopedLoopConsts(&inner)
	if !inner.has(ctrlLoopConstScope) || f.iconstN != 2 {
		t.Fatal("missing nested lease")
	}
	f.releaseScopedLoopConsts(uint8(inner.flags >> 12))
	if f.iconstN != 1 || !f.reserved.has(outerReg) {
		t.Fatal("nested release lost outer cache")
	}
	if _, ok := f.cachedIntConst(storage{typ: mtI32, cval: h.loopIntConst[1]}); ok {
		t.Fatal("nested constant escaped")
	}
	f.releaseScopedLoopConsts(uint8(outer.flags >> 12))
	if f.iconstN != 0 || f.reserved != original {
		t.Fatal("lease reservation leaked")
	}
	if f.intervalRegLimit != originalLimit {
		t.Fatalf("released leases retained regional register limit %d, want %d", f.intervalRegLimit, originalLimit)
	}
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
}
func TestScopedLoopConstRequiresCallFreeProof(t *testing.T) {
	h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 1, loopIntConst: [4]int64{0x1234567}}
	f := fn{a: &a64.Asm{}, scopedConstHints: scopedConstHintsFrom(&h), policy: currentCodegenPolicy()}
	fr := ctrlFrame{kind: cfLoop, flags: 1 << 12}
	f.preloadScopedLoopConsts(&fr)
	if f.iconstN != 0 || fr.flags>>12 != 0 {
		t.Fatal("cache admitted without call-free proof")
	}
}
func TestScopedLoopConstConsumerDoesNotAdvanceReader(t *testing.T) {
	h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 1, loopIntConst: [4]int64{0x1234567}}
	for _, op := range []byte{0x6c, 0x71, 0x1a} {
		body := append(wasmtest.SLEB32(0x1234567), op)
		r := wasm.ReaderFrom(body)
		selected := scopedConstHintsFrom(&h)
		u := scopedLoopConstUses{hints: &selected}
		u.note(0x41, &r)
		if r.Offset() != 0 {
			t.Fatal("constant probe advanced shared reader")
		}
		expected := uint8(1)
		if op == 0x1a {
			expected = 0
		}
		if u.mask != expected {
			t.Fatalf("consumer=%x uses=%x want=%x", op, u.mask, expected)
		}
	}
}

func TestScopedLoopConstSkipsStrengthReducedConsumer(t *testing.T) {
	h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 1, loopIntConst: [4]int64{-2147483648}}
	r := wasm.ReaderFrom(append(wasmtest.SLEB32(-2147483648), 0x6c))
	selected := scopedConstHintsFrom(&h)
	u := scopedLoopConstUses{hints: &selected}
	u.note(0x41, &r)
	if u.mask != 0 || r.Offset() != 0 {
		t.Fatal("shift-only multiplier leased a register or moved cursor")
	}
}

func TestScopedLoopConstExecutionAcrossGuestCalls(t *testing.T) {
	old := scopedLoopConstsEnabled
	scopedLoopConstsEnabled = true
	defer func() { scopedLoopConstsEnabled = old }()
	const factor = int32(0x1234567)
	caller := []byte{1, 1, 0x7f, 0x20, 0, 0x10, 1, 0x1a, 0x41, 1, 0x21, 1, 0x02, 0x40, 0x03, 0x40,
		0x20, 0, 0x45, 0x0d, 1, 0x20, 1, 0x41}
	caller = append(caller, wasmtest.SLEB32(factor)...)
	caller = append(caller, 0x6c, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x10, 1, 0x0b)
	// Real callee writes several local pins so preservation cannot rely on an
	// accidentally untouched register bank or the pin-preserving leaf ABI.
	callee := []byte{1, 8, 0x7f}
	for i := byte(1); i <= 8; i++ {
		callee = append(callee, 0x20, 0, 0x41, i, 0x6a, 0x21, i)
	}
	callee = append(callee, 0x20, 0, 0x41, 17, 0x6a, 0x0b)
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: caller}, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: callee})
	for _, enabled := range []bool{false, true} {
		// Per-compilation selection must win over the opposite process default.
		scopedLoopConstsEnabled = !enabled
		for _, guard := range []bool{false, true} {
			for _, n := range []uint64{0, 1, 2, 5, 9} {
				want := uint32(1)
				for i := uint64(0); i < n; i++ {
					want *= uint32(factor)
				}
				want += 17
				var stats ModuleStats
				var statsOut *ModuleStats
				if diagnosticsEnabled {
					statsOut = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Stats: statsOut, ElideBoundsChecks: guard, Optimizations: map[string]bool{"inline": false, "loop-int-const": true, "scoped-loop-int-const": enabled}}, n)
				if err != nil || got != uint64(want) {
					t.Fatalf("cache=%v guard=%v n=%d got=%x want=%x err=%v", enabled, guard, n, got, want, err)
				}
				if diagnosticsEnabled {
					hits := stats.Funcs[0].Peephole["scoped-loop-int-const"]
					if enabled && hits == 0 || !enabled && hits != 0 {
						t.Fatalf("cache=%v scoped admissions=%d", enabled, hits)
					}
				}
			}
		}
	}
}
