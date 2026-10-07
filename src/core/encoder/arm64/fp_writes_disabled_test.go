//go:build !wago_regalloccheck

package arm64

import "testing"

func TestFPWritesOrdinaryObserverIsInert(t *testing.T) {
	var a Asm
	if old := a.ObserveFPWrites(func(uint32) { t.Fatal("ordinary build invoked FP observer") }); old != nil {
		t.Fatal("ordinary build retained FP observer")
	}
	a.Fadd(3, 4, 5, true)
	a.NeonInsD(3, X0, 1)
	a.Bl()
	if old := a.ObserveFPWrites(nil); old != nil {
		t.Fatal("ordinary build retained FP observer")
	}
}
