package compactgc

import (
	"testing"
	"unsafe"
)

func TestCohortDiagnosticFootprint(t *testing.T) {
	var c Collector
	t.Logf("collector=%d diagnostic_state=%d first_regular_field=%d enabled=%v", unsafe.Sizeof(c), unsafe.Sizeof(c.cohortDiagnosticState), unsafe.Offsetof(c.retiredNurseryAllocations), cohortDiagnostics)
	if !cohortDiagnostics && (unsafe.Sizeof(c.cohortDiagnosticState) != 0 || unsafe.Offsetof(c.retiredNurseryAllocations) != 0) {
		t.Fatal("disabled diagnostic changes leading layout")
	}
}
