//go:build arm64 && wago_regalloccheck

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Only assembler output is inspected; no deliberately clobbering guest code runs.
func TestRegallocImmutableFPRejectsWritesOutsideWindows(t *testing.T) {
	for name, emit := range map[string]func(*enc.Asm){
		"scalar":      func(a *enc.Asm) { a.Fadd(3, 4, 5, true) },
		"partial":     func(a *enc.Asm) { a.FmovReg(3, 4, false) },
		"lane":        func(a *enc.Asm) { a.NeonInsD(3, X0, 1) },
		"packed":      func(a *enc.Asm) { a.NeonAddB(3, 4, 5) },
		"reduction":   func(a *enc.Asm) { a.NeonAddvB(3, 4) },
		"literal":     func(a *enc.Asm) { a.LdrLiteralF(3, true) },
		"pair-second": func(a *enc.Asm) { a.LdpQ(2, 3, SP, 0) },
		"call":        func(a *enc.Asm) { a.Blr(X0) },
	} {
		t.Run(name, func(t *testing.T) {
			f := fn{a: &enc.Asm{}}
			f.checkImmutable(3, true, 16)
			defer f.checkEndLifetimes()
			requireAllocationFailure(t, "immutable FP", func() { emit(f.a) })
		})
	}
}

func TestRegallocImmutableFPReadsAndCleanup(t *testing.T) {
	f := fn{a: &enc.Asm{}}
	f.checkImmutable(3, true, 16)
	defer f.checkEndLifetimes()
	f.a.MovImm64(3, 0)
	f.a.StrQ(SP, 0, 3)
	f.a.NeonUmovD(X0, 3, 1)
	f.a.Fcmp(3, 4, true)
	f.checkEndLifetimes()
	f.checkEndLifetimes()
	f.a.Fadd(3, 4, 5, true)
}

func TestRegallocImmutableFPObserverCleanupAndTerminalScope(t *testing.T) {
	a := &enc.Asm{}
	observed := 0
	a.ObserveFPWrites(func(uint32) { observed++ })
	f := fn{a: a}
	f.checkImmutable(3, true, 16)
	a.Fadd(4, 5, 6, true)
	if observed != 1 {
		t.Fatal("enclosing observer lost")
	}
	func() { saved := f.checkTerminalWrites(); defer f.checkRestoreWrites(saved); a.Fadd(3, 4, 5, true) }()
	if observed != 2 {
		t.Fatal("terminal scope lost enclosing observer")
	}
	requireAllocationFailure(t, "immutable FP", func() { defer f.checkEndLifetimes(); a.Fadd(3, 4, 5, true) })
	before := observed
	a.Fadd(3, 4, 5, true)
	if observed != before+1 || f.fpObserverActive || f.fpObserverPrevious != nil || f.immutableFPMask != 0 {
		t.Fatal("panic retained observer state")
	}
	g := fn{a: a}
	g.checkImmutable(3, true, 16)
	requireAllocationFailure(t, "immutable FP", func() { a.Fadd(3, 4, 5, true) })
	g.checkEndLifetimes()
	g.checkEndLifetimes()
	before = observed
	a.Fadd(3, 4, 5, true)
	if observed != before+1 {
		t.Fatal("reuse lost enclosing observer")
	}
}

func TestRegallocImmutableFPUnsupportedWidth(t *testing.T) {
	f := fn{a: &enc.Asm{}}
	defer f.checkEndLifetimes()
	requireAllocationFailure(t, "unsupported", func() { f.checkImmutable(3, true, 32) })
	if f.fpObserverActive || f.immutableFPMask != 0 {
		t.Fatal("unsupported reservation installed observer")
	}
}

func TestRegallocImmutableFPTerminalResultsRestoreBody(t *testing.T) {
	for _, kind := range []string{"direct", "branch", "epilogue"} {
		t.Run(kind, func(t *testing.T) {
			f := fn{a: &enc.Asm{}, s: newStack(), sc: &scratch{}, resultFloat: true, resultF64: true, singleRegResult: true, ft: &wasm.CompType{Results: []wasm.ValType{wasm.V128}}}
			f.checkImmutable(0, true, 16)
			defer f.checkEndLifetimes()
			switch kind {
			case "direct":
				f.pushReg(4, mtF64)
				f.placeSingleResult()
			case "branch":
				f.branchJump(&ctrlFrame{kind: cfFunc})
			case "epilogue":
				f.epilogue()
			}
			requireAllocationFailure(t, "immutable FP", func() { f.a.Fadd(0, 4, 5, true) })
		})
	}
}

func TestRegallocImmutableFPVisibleInsideTransferWindows(t *testing.T) {
	for _, kind := range []string{"flush", "slots", "abi"} {
		t.Run(kind, func(t *testing.T) {
			f := fn{a: &enc.Asm{}, s: newStack()}
			f.checkImmutable(3, true, 16)
			defer f.checkEndLifetimes()
			switch kind {
			case "flush":
				f.checkBeginFlush(nil)
			case "slots":
				f.checkBeginSlots(0, 1, 1)
			case "abi":
				f.checkBeginRegMoves([]regMove{{src: 4, dst: 4}}, true)
			}
			// The intentionally interrupted window has no normal completion.
			defer f.a.ObserveRegalloc(nil)
			requireAllocationFailure(t, "immutable FP", func() { f.a.Fadd(3, 4, 5, true) })
		})
	}
}

func TestRegallocImmutableFPAllowsTerminalTrapStubs(t *testing.T) {
	f := cachedLifetimeFunction(t)
	f.sc = &scratch{}
	f.checkImmutable(3, true, 16)
	f.trapAlways(trapUnreachable)
	f.emitTrapStubs()
	requireAllocationFailure(t, "immutable FP", func() { f.a.Fadd(3, 4, 5, true) })
}
