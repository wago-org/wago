//go:build !wago_regalloccheck

package amd64

import "testing"

func TestFPWritesOrdinaryObserverIsInert(t *testing.T) {
	var a Asm
	if old := a.ObserveFPWrites(func(uint32) { t.Fatal("ordinary build invoked FP observer") }); old != nil {
		t.Fatal("ordinary build retained FP observer")
	}
	a.FAdd(3, 4, true)
	a.XPternlogd(3, 4, 5, 0x96)
	a.CallReg(RAX)
	if old := a.ObserveFPWrites(nil); old != nil {
		t.Fatal("ordinary build retained FP observer")
	}
}
