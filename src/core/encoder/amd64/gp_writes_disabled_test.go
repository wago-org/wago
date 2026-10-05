//go:build !wago_regalloccheck

package amd64

import "testing"

func TestGPWritesOrdinaryNoObserver(t *testing.T) {
	var a Asm
	if old := a.ObserveGPWrites(func(uint32) { t.Fatal("ordinary build invoked GP observer") }); old != nil {
		t.Fatal("ordinary build retained GP observer")
	}
	a.MovImm64(R13, 0x1234567812345678)
	a.Add64(R13, R9)
	a.CallReg(R13)
	if old := a.ObserveGPWrites(nil); old != nil {
		t.Fatal("ordinary build retained GP observer")
	}
}
