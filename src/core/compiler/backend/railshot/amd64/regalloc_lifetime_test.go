//go:build amd64 && wago_regalloccheck

package amd64

import (
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func cachedLifetimeFunction(t *testing.T) *fn {
	t.Helper()
	f := &fn{a: &enc.Asm{}, s: newStack(), policy: currentCodegenPolicy()}
	h := funcHintView{loopIntConsts: &loopIntConstHintEntry{bits: [2]int64{0x123456789abcdef}, count: 1}}
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 1 {
		t.Fatal("expected actual integer cache preload")
	}
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
	root := f.pushReg(RAX, mtI64)
	f.checkBeginFlush([]*elem{root})
	defer f.a.ObserveRegalloc(nil)
	requireAllocationFailure(t, "immutable GP", func() { f.a.MovImm64(f.iconsts[0].reg, 0) })
}

func TestRegallocImmutableGPSeesDirectEncoderCall(t *testing.T) {
	f := cachedLifetimeFunction(t)
	requireAllocationFailure(t, "immutable GP", func() { f.a.CallReg(RAX) })
}
