package compactgc

import (
	"testing"
	"unsafe"
)

func TestAge2PolicyFootprint(t *testing.T) {
	var c Collector
	t.Logf("collector=%d policy=%d first_regular_offset=%d enabled_build=%v", unsafe.Sizeof(c), unsafe.Sizeof(c.age2State), unsafe.Offsetof(c.retiredNurseryAllocations), age2Build)
	if !age2Build && unsafe.Sizeof(c.age2State) != 0 {
		t.Fatal("default footprint changed")
	}
	if !age2Build && !cohortDiagnostics && !shadowDiagnostics && unsafe.Offsetof(c.retiredNurseryAllocations) != 0 {
		t.Fatal("default leading layout changed")
	}
}
