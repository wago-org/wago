//go:build wago_regalloccheck

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	"strings"
	"testing"
)

func requireVectorLost(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		err := recover()
		if err == nil || !strings.Contains(fmt.Sprint(err), "regalloccheck:") {
			t.Fatalf("expected vector corruption, got %v", err)
		}
	}()
	action()
}

func TestRegallocScalarLoadKillsVectorUpperBytes(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var state regalloccheck.State
		var a Asm
		loc := regalloccheck.Register(regalloccheck.FP, 1)
		vector := state.Seed(loc, 16)
		a.ObserveRegalloc(state.Apply)
		a.FStoreDisp(SP, 32, 1, wide)
		a.FLoadDisp(1, SP, 32, wide)
		size := 4
		if wide {
			size = 8
		}
		state.Expect("scalar preserved", loc, vector[:size])
		a.VMovdquStoreDisp(SP, 64, 1)
		requireVectorLost(t, func() { state.Expect("scalar load destroyed upper lanes", regalloccheck.Slot(64), vector) })
	}
}

func TestRegallocScalarRegisterMoveUpperBytes(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var state regalloccheck.State
		var a Asm
		loc := regalloccheck.Register(regalloccheck.FP, 1)
		vector := state.Seed(loc, 16)
		a.ObserveRegalloc(state.Apply)
		a.FmovReg(1, 1, wide)
		requireVectorLost(t, func() { state.Expect("scalar register move destroys upper lanes", loc, vector) })
	}
}

func TestRegallocCrossBankScalarMoves(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var state regalloccheck.State
		var a Asm
		fp, gp := regalloccheck.Register(regalloccheck.FP, 1), regalloccheck.Register(regalloccheck.GP, 2)
		vector := state.Seed(fp, 16)
		scalar := state.Seed(gp, 8)
		a.ObserveRegalloc(state.Apply)
		a.FmovFromGpr(1, 2, wide)
		size := 4
		if wide {
			size = 8
		}
		state.Expect("copied to FP", fp, scalar[:size])
		// Check just the old upper lanes, independently of the changed low value.
		upper := fp
		upper.Byte = uint8(size)
		requireVectorLost(t, func() { state.Expect("cleared FP upper lanes", upper, vector[size:]) })
		a.FmovToGpr(3, 1, wide)
		state.Expect("copied to GP", regalloccheck.Register(regalloccheck.GP, 3), scalar[:size])
	}
}
