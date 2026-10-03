//go:build arm64 && wago_regalloccheck

package arm64

import (
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func cachedLifetimeFunction(t *testing.T) *fn {
	t.Helper()
	f := &fn{a: &enc.Asm{}, s: newStack(), policy: currentCodegenPolicy()}
	h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 2, loopIntConst: [4]int64{0x123456789abcdef}}
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 1 {
		t.Fatal("expected actual integer cache preload")
	}
	t.Cleanup(f.checkEndLifetimes)
	return f
}

func TestRegallocImmutableGPRejectsConcreteOverwrite(t *testing.T) {
	f := cachedLifetimeFunction(t)
	reg := f.iconsts[0].reg
	// The allocator reservation remains plausible while the encoder writes it.
	requireAllocationFailure(t, "immutable GP", func() { f.a.MovImm64(reg, 0) })
}

func TestRegallocImmutableGPRejectsWriteBeforeRestore(t *testing.T) {
	f := cachedLifetimeFunction(t)
	reg := f.iconsts[0].reg
	requireAllocationFailure(t, "immutable GP", func() {
		f.a.MovImm64(reg, 0)
		f.a.MovImm64(reg, 0x123456789abcdef)
	})
}

func TestRegallocImmutableGPVisibleInsideTransferWindow(t *testing.T) {
	f := cachedLifetimeFunction(t)
	root := f.pushReg(X0, mtI64)
	f.checkBeginFlush([]*elem{root})
	defer f.a.ObserveRegalloc(nil)
	requireAllocationFailure(t, "immutable GP", func() { f.a.MovImm64(f.iconsts[0].reg, 0) })
}

func TestRegallocImmutableGPSeesDirectEncoderCall(t *testing.T) {
	f := cachedLifetimeFunction(t)
	requireAllocationFailure(t, "immutable GP", func() { f.a.Blr(X0) })
}

func TestRegallocImmutableGPVisibleInsideSlotsAndABI(t *testing.T) {
	for _, window := range []string{"slots", "ABI"} {
		t.Run(window, func(t *testing.T) {
			f := cachedLifetimeFunction(t)
			var finish func()
			if window == "slots" {
				finish = f.checkBeginSlots(0, 1, 1)
			} else {
				finish = f.checkBeginRegMoves([]regMove{{src: X0, dst: X0}}, false)
			}
			defer f.a.ObserveRegalloc(nil)
			// Keep finish reachable without checking the intentionally interrupted move.
			if finish == nil {
				t.Fatal("missing transfer window")
			}
			requireAllocationFailure(t, "immutable GP", func() { f.a.MovImm64(f.iconsts[0].reg, 0) })
		})
	}
}

func TestRegallocImmutableGPKeepsRegisterBanksSeparate(t *testing.T) {
	f := cachedLifetimeFunction(t)
	reg := f.iconsts[0].reg
	f.a.FmovFromGpr(reg, X0, true)
	requireAllocationFailure(t, "immutable GP", func() { f.a.MovImm64(reg, 0) })
}

func TestRegallocImmutableGPRestoresObserverOnPanicAndReuse(t *testing.T) {
	a := &enc.Asm{}
	observed := 0
	previous := func(uint32) { observed++ }
	a.ObserveGPWrites(previous)
	f := fn{a: a}
	a.MovImm64(X25, 42)
	f.checkImmutable(X25, false, 8)
	before := observed
	a.MovImm64(X0, 0)
	if observed != before+1 {
		t.Fatal("enclosing observer lost")
	}
	requireAllocationFailure(t, "immutable GP", func() {
		defer f.checkEndLifetimes()
		a.MovImm64(X25, 0)
	})
	before = observed
	a.MovImm64(X25, 0)
	if observed != before+1 || f.gpObserverActive || f.immutableGPMask != 0 {
		t.Fatal("panic retained function observer")
	}
	// The same assembler can serve another function after a failed attempt.
	g := fn{a: a}
	g.checkImmutable(X0, false, 8)
	a.MovImm64(X25, 0)
	requireAllocationFailure(t, "immutable GP", func() { a.MovImm64(X0, 0) })
	g.checkEndLifetimes()
	g.checkEndLifetimes()
	before = observed
	a.MovImm64(X0, 0)
	if observed != before+1 {
		t.Fatal("reuse did not restore previous observer")
	}
}

func TestRegallocTerminalScopeRestoresBodyReservation(t *testing.T) {
	f := cachedLifetimeFunction(t)
	reg := f.iconsts[0].reg
	func() { saved := f.checkTerminalGPWrites(); defer f.checkRestoreGPWrites(saved); f.a.MovImm64(reg, 0) }()
	requireAllocationFailure(t, "immutable GP", func() { f.a.MovImm64(reg, 0) })
}
