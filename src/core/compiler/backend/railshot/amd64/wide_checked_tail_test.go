//go:build amd64

package amd64

import "testing"

func TestWideCheckedTailHomes(t *testing.T) {
	f := fn{nLocals: 256, locals: make([]localDef, 256)}
	var entry [256]regionEntryLocal
	for i := range f.locals {
		f.locals[i] = localDef{reg: Reg(i % 16), state: lsMem}
		entry[i] = regionEntryLocal{f.locals[i].reg, f.locals[i].state}
	}
	if !f.regionCheckedTailHomes(&entry) {
		t.Fatal("identical homes rejected")
	}
	for i := range entry {
		entry[i].reg ^= 1
		if f.regionCheckedTailHomes(&entry) {
			t.Fatal("changed register admitted", i)
		}
		entry[i].reg ^= 1
		entry[i].state = lsReg
		if f.regionCheckedTailHomes(&entry) {
			t.Fatal("changed frame ownership admitted", i)
		}
		entry[i].state = lsMem
	}
}
