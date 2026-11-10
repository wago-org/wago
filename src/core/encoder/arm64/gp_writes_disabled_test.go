//go:build !wago_regalloccheck

package arm64

import "testing"

func TestGPWritesOrdinaryObserverIsInert(t *testing.T) {
	var a Asm
	if old := a.ObserveGPWrites(func(uint32) { t.Fatal("ordinary build called GP observer") }); old != nil {
		t.Fatal("ordinary build retained observer state")
	}
	a.Add64(X6, X7, X8)
	a.LdpPost(X6, X7, SP, 16)
	a.Bl()
	if old := a.ObserveGPWrites(nil); old != nil {
		t.Fatal("ordinary build retained observer state")
	}
}
