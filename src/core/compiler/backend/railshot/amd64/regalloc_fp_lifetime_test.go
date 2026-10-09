//go:build amd64 && wago_regalloccheck

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestRegallocImmutableFPScratchBorrow(t *testing.T) {
	for _, kind := range []string{"lane", "binary"} {
		t.Run(kind, func(t *testing.T) {
			f := fn{a: &enc.Asm{}, s: newStack(), sc: &scratch{},
				fconsts: []floatConstReg{{reg: 0}, {reg: 1}},
				vconsts: []v128ConstReg{{reg: 2}, {reg: 3}, {reg: 4}, {reg: 5}},
			}
			defer f.checkEndLifetimes()
			var writes uint32
			f.a.ObserveFPWrites(func(mask uint32) {
				writes |= mask
				if mask&0x3f != 0 {
					t.Fatal("scratch sequence wrote an immutable cache")
				}
			})
			for r := Reg(0); r < 6; r++ {
				f.checkImmutable(r, true, 16)
			}
			switch kind {
			case "lane":
				f.extractSIMDLane(RAX, 6, 1, 8)
				if writes != 1<<7 {
					t.Fatalf("lane writes = %#x, want XMM7", writes)
				}
			case "binary":
				f.legacySIMDBinary(opVPaddd, 6, 7, 6)
				if writes != 1<<6|1<<8 {
					t.Fatalf("binary writes = %#x, want XMM6 and XMM8", writes)
				}
			}
		})
	}
}

func TestRegallocImmutableGPSIMDScratchBorrow(t *testing.T) {
	for _, kind := range []string{"fallback", "insert-rax", "insert-rdx", "insert-rcx"} {
		t.Run(kind, func(t *testing.T) {
			f := fn{a: &enc.Asm{}, s: newStack(), sc: &scratch{}}
			defer f.checkEndLifetimes()
			// Use the complete production cache-candidate set, not only R11.
			for _, r := range []Reg{R12, R13, R14, R15, R9, R10, R11, RDI, RSI} {
				f.checkImmutable(r, false, 8)
			}
			switch kind {
			case "fallback":
				f.simdFallback(0x00, 0, 1, 2)
			case "insert-rax":
				f.insertSIMDLane(0, RAX, 1, 8)
			case "insert-rdx":
				f.insertSIMDLane(0, RDX, 1, 8)
			case "insert-rcx":
				f.insertSIMDLane(0, RCX, 1, 8)
			}
		})
	}
}

// These controls only emit bytes; they never execute the deliberately
// clobbering instruction stream.
func TestRegallocImmutableFPRejectsWritesOutsideWindows(t *testing.T) {
	for name, emit := range map[string]func(*enc.Asm){
		"scalar":  func(a *enc.Asm) { a.FAdd(3, 4, true) },
		"partial": func(a *enc.Asm) { a.FMov(3, 4, false) },
		"lane":    func(a *enc.Asm) { a.Pinsrq(3, RAX, 1) },
		"packed":  func(a *enc.Asm) { a.VPaddb(3, 4, 5) },
		"shift":   func(a *enc.Asm) { a.VPsllwImm(3, 4, 1) },
		"wide":    func(a *enc.Asm) { a.YPaddb(3, 4, 5) },
		"evex":    func(a *enc.Asm) { a.XPrordImm(3, 4, 1) },
		"literal": func(a *enc.Asm) { a.MovsRipPlaceholder(3, true) },
		"call":    func(a *enc.Asm) { a.CallReg(RAX) },
	} {
		t.Run(name, func(t *testing.T) {
			f := fn{a: &enc.Asm{}}
			f.checkImmutable(3, true, 16)
			defer f.checkEndLifetimes()
			requireAllocationFailure(t, "immutable FP", func() { emit(f.a) })
		})
	}
}

func TestRegallocImmutableFPReadsRetirementAndCleanup(t *testing.T) {
	f := fn{a: &enc.Asm{}}
	f.checkImmutable(3, true, 16)
	defer f.checkEndLifetimes()
	f.a.MovImm64(3, 0) // the GP bank is independent
	f.a.VMovdquStoreDisp(RSP, 0, 3)
	f.a.Pextrq(RAX, 3, 1)
	f.a.VZeroUpper() // changes no low-128-bit cache bytes
	f.checkReleaseImmutable(3, true)
	f.a.FAdd(3, 4, true)
	f.checkImmutable(3, true, 16)
	f.checkEndLifetimes()
	f.checkEndLifetimes()
	f.a.FAdd(3, 4, true)
}

func TestRegallocImmutableFPObserverCleanupAndTerminalScope(t *testing.T) {
	a := &enc.Asm{}
	observed := 0
	a.ObserveFPWrites(func(uint32) { observed++ })
	f := fn{a: a}
	f.checkImmutable(3, true, 16)
	a.FAdd(4, 5, true)
	if observed != 1 {
		t.Fatal("enclosing observer lost")
	}
	func() { saved := f.checkTerminalWrites(); defer f.checkRestoreWrites(saved); a.FAdd(3, 4, true) }()
	if observed != 2 {
		t.Fatal("terminal scope lost enclosing observer")
	}
	requireAllocationFailure(t, "immutable FP", func() { defer f.checkEndLifetimes(); a.FAdd(3, 4, true) })
	before := observed
	a.FAdd(3, 4, true)
	if observed != before+1 || f.fpObserverActive || f.fpObserverPrevious != nil || f.immutableFPMask != 0 {
		t.Fatal("panic retained observer state")
	}
	g := fn{a: a}
	g.checkImmutable(3, true, 16)
	requireAllocationFailure(t, "immutable FP", func() { a.FAdd(3, 4, true) })
	g.checkEndLifetimes()
	g.checkEndLifetimes()
	before = observed
	a.FAdd(3, 4, true)
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
			requireAllocationFailure(t, "immutable FP", func() { f.a.FAdd(0, 4, true) })
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
			requireAllocationFailure(t, "immutable FP", func() { f.a.FAdd(3, 4, true) })
		})
	}
}

func TestRegallocImmutableFPRetirementKeepsOuterCache(t *testing.T) {
	f := fn{a: &enc.Asm{}}
	defer f.checkEndLifetimes()
	f.checkImmutable(3, true, 16)
	f.checkImmutable(4, true, 8)
	f.checkReleaseImmutable(4, true)
	f.a.FAdd(4, 5, true)
	requireAllocationFailure(t, "immutable FP", func() { f.a.FAdd(3, 5, true) })
}

func TestRegallocImmutableFPAllowsTerminalTrapStubs(t *testing.T) {
	f := cachedLifetimeFunction(t)
	f.sc = &scratch{}
	f.checkImmutable(3, true, 16)
	f.trapAlways(trapUnreachable)
	f.emitTrapStubs()
	requireAllocationFailure(t, "immutable FP", func() { f.a.FAdd(3, 4, true) })
}
