//go:build !wago_profile

package shared

import (
	"testing"
	"unsafe"
)

func TestScalarSourcesPreserveOrdinaryWorkerLayout(t *testing.T) {
	type previous struct {
		nodes                       []scalarNode
		stack, locals               []scalarID
		widths                      []bool
		controls                    []scalarControl
		freeSlots                   []int
		owners                      [64]scalarID
		nextSlot, tempBase, maxSlot int
		regs                        []uint8
		reserved                    uint64
		target                      ScalarTarget
		Peak, Discarded             uint64
		Spills, Reloads             int
	}
	if unsafe.Sizeof(scalarState{}) != unsafe.Sizeof(previous{}) || unsafe.Offsetof(scalarState{}.nodes) != 0 {
		t.Fatal("ordinary scalar worker layout grew")
	}
	var state ScalarState
	var recorder ScalarSourceRecorder
	if _, err := state.CompileScalarWithSources(nil, ScalarSummary{}, nil, 0, &scalarTestTarget{}, &recorder, 0, 0); err == nil || state.Memory() != 0 || recorder.Memory() != 0 {
		t.Fatal("ordinary source request did not fail without allocation")
	}
}
