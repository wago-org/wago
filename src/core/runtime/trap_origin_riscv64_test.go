//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"errors"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestRV64TrapOriginFreshnessAtRejectedAndInterruptedEntries(t *testing.T) {
	e, jm, arena := productionFixtureRV64(t, 0)
	trap := arena.Alloc(TrapBufferBytes)
	if err := jm.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	var a enc.Asm
	a.Ret()
	entry := productionImageRV64(t, &a)
	seed := func(interrupted bool) {
		clear(trap)
		binary.LittleEndian.PutUint64(trap[8:16], 0x12345678)
		binary.LittleEndian.PutUint64(trap[16:24], uint64(37)<<32|8)
		if interrupted {
			storeTrap(trap, uint32(TrapInterrupted))
		}
	}
	for _, bounded := range []bool{false, true} {
		seed(false)
		var err error
		if bounded {
			err = e.CallPreparedBounded(0, nil, jm.LinMemBase(), trap, nil)
		} else {
			err = e.CallPrepared(0, nil, jm.LinMemBase(), trap, nil)
		}
		var caught *TrapError
		if !errors.As(err, &caught) || caught.Code != TrapStackFenceBreached || len(caught.Frames) != 0 || binary.LittleEndian.Uint64(trap[8:16]) != 0 || binary.LittleEndian.Uint64(trap[16:24]) != 0 {
			t.Fatal("bridge rejection retained previous producer frame", err)
		}
		seed(true)
		if bounded {
			err = e.CallPreparedBounded(entry, nil, jm.LinMemBase(), trap, nil)
		} else {
			err = e.CallPrepared(entry, nil, jm.LinMemBase(), trap, nil)
		}
		if !errors.As(err, &caught) || caught.Code != TrapInterrupted || len(caught.Frames) != 0 {
			t.Fatal("interrupted entry retained previous producer frame", err)
		}
	}
	seed(true)
	clearTrapUnlessInterrupted(trap)
	if loadTrap(trap) != uint32(TrapInterrupted) || binary.LittleEndian.Uint64(trap[8:16]) != 0 || binary.LittleEndian.Uint64(trap[16:24]) != 0 {
		t.Fatal("host-loop reset lost interruption or retained provenance")
	}
	ctrl := arena.Alloc(HostCtrlFrameBytes)
	seed(true)
	err := e.CallWithHostBase(entry, nil, jm.LinMemBase(), trap, nil, ctrl, func(uintptr, uint32, []uint64, []uint64) { t.Error("interrupted no-op dispatched host") })
	var caught *TrapError
	if !errors.As(err, &caught) || caught.Code != TrapInterrupted || len(caught.Frames) != 0 {
		t.Fatal("host entry retained producer source", err)
	}
}
