//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import (
	"testing"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestInstanceContextRoundTripLeavesMemoryAndInvocationState(t *testing.T) {
	jm, err := NewJobMemoryGrowable(65536, 4*65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()

	jm.SetStackFence(0x1111)
	trap := make([]byte, 8)
	if err := jm.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	beforeBytes := jm.curBytes()
	beforeFence := jm.getU64(offStackFence)
	beforeTrap := jm.getU64(abi.TrapCellPtrOffset)

	want := InstanceContext{
		CustomCtx:      1,
		TablePtr:       2,
		FuncRefDescPtr: 3,
		PassiveElemPtr: 4,
		GlobalsPtr:     5,
		PassiveDataPtr: 6,
		TableDirPtr:    7,
		ImportDispatch: 8,
	}
	jm.BindInstanceContext(want)
	if got := jm.CaptureInstanceContext(); got != want {
		t.Fatalf("InstanceContext = %+v, want %+v", got, want)
	}
	if jm.curBytes() != beforeBytes || jm.getU64(offStackFence) != beforeFence || jm.getU64(abi.TrapCellPtrOffset) != beforeTrap {
		t.Fatal("binding instance context changed memory or invocation state")
	}
}
