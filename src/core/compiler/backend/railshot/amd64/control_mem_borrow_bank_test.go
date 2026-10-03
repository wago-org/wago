//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestControlIntervalAddressAvoidsFixedRegisters(t *testing.T) {
	f := &fn{a: &encoderamd64.Asm{}, s: newStack()}
	for r := range f.intervalOwner {
		f.intervalOwner[r] = -1
	}
	f.intervalOwner[R12] = 0
	for _, r := range gpAlloc {
		if r != RAX && r != RDX && r != RCX && r != R8 {
			f.reserved = f.reserved.add(r)
		}
	}
	reserved := f.reserved
	load := f.pushValue(memRefStorage(R12, 7, 4, false, false, 0))
	if got := f.detachControlIntervalAddresses(); got != maskOf(R8) {
		t.Fatalf("carrier mask = %#x, want R8", got)
	}
	if load.st.kind != stMemRef || load.st.reg != R8 || load.st.memBorrow() != -1 || f.regUser[R8] != load {
		t.Fatalf("address ownership was not transferred: %+v", load.st)
	}
	if f.reserved != reserved {
		t.Fatal("address detachment changed caller reservations")
	}
	want := &encoderamd64.Asm{}
	want.MovReg64(R8, R12)
	if !bytes.Equal(f.a.B, want.B) {
		t.Fatalf("detachment emitted more than an address copy: %x, want %x", f.a.B, want.B)
	}
}
