//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import (
	"testing"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestJobMemoryHasTrapCellDetectsCrossInstanceOverwrite(t *testing.T) {
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	trap := make([]byte, 8)
	if jm.HasTrapCell(trap) {
		t.Fatal("fresh job memory unexpectedly has the trap cell bound")
	}
	if err := jm.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	if !jm.HasTrapCell(trap) {
		t.Fatal("bound trap cell was not recognized")
	}
	jm.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(trap))+8)
	if jm.HasTrapCell(trap) {
		t.Fatal("overwritten trap-cell pointer was not detected")
	}
}
