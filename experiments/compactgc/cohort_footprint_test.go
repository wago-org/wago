package compactgc

import (
	"testing"
	"unsafe"
)

func TestCohortDiagnosticFootprint(t *testing.T) {
	var c Collector
	t.Logf("collector=%d diagnostic_state=%d first_regular_field=%d enabled=%v", unsafe.Sizeof(c), unsafe.Sizeof(c.cohortDiagnosticState), unsafe.Offsetof(c.retiredNurseryAllocations), cohortDiagnostics)
	if !shadowDiagnostics && unsafe.Sizeof(c.shadowDiagnosticState) != 0 {
		t.Fatal("disabled shadow changes layout")
	}
	if !cohortDiagnostics && unsafe.Sizeof(c.cohortDiagnosticState) != 0 {
		t.Fatal("disabled diagnostic changes leading layout")
	}
}
