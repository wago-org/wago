//go:build wago_regalloccheck && !tinygo

package riscv64

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func TestAtomicEffectsNeverProveConditionalCopyOrZeroNormalization(t *testing.T) {
	var state regalloccheck.State
	gp5 := regalloccheck.Register(regalloccheck.GP, 5)
	gp6 := regalloccheck.Register(regalloccheck.GP, 6)
	gp7 := regalloccheck.Register(regalloccheck.GP, 7)
	state.Seed(gp5, 8)
	status := state.Seed(gp6, 8)
	source := state.Seed(gp7, 8)
	frame := state.Seed(regalloccheck.Slot(0), 8)
	var a Asm
	a.ObserveRegalloc(state.Apply)
	a.LrW(X5, SP, AtomicAcquire)
	if !reflect.DeepEqual(state.Read(gp5, 8), make(regalloccheck.Value, 8)) {
		t.Fatal("sign-extended LR.W retained identity")
	}
	a.ScW(X6, X7, SP, AtomicRelease)
	if !reflect.DeepEqual(state.Read(regalloccheck.Slot(0), 4), make(regalloccheck.Value, 4)) {
		t.Fatal("conditional SC.W proved a store")
	}
	state.Expect("unwritten upper frame word", regalloccheck.Slot(4), frame[4:])
	state.Expect("SC input preserved", gp7, source)
	if reflect.DeepEqual(state.Read(gp6, 8), status) {
		t.Fatal("SC status retained prior carrier")
	}
	a.ScD(Zero, X7, SP, AtomicRelease)
	if !reflect.DeepEqual(state.Read(regalloccheck.Slot(0), 8), make(regalloccheck.Value, 8)) {
		t.Fatal("conditional SC.D retained frame fact")
	}
	state.Expect("discarded status preserves source", gp7, source)
}

func TestAtomicInvalidStickyAndUnalignedEmitNoEffects(t *testing.T) {
	for _, operation := range []string{"lr.w", "lr.d", "sc.w", "sc.d"} {
		for _, tc := range []struct {
			dst, value, base Reg
			order            AtomicOrder
			prefix           []byte
		}{
			{32, X7, X6, AtomicAcquire, nil}, {X5, X7, 32, AtomicRelease, nil}, {X5, X7, X6, 4, nil}, {X5, X7, X6, AtomicAcquire, []byte{0}},
		} {
			a := Asm{B: tc.prefix}
			a.ObserveGPWrites(func(uint32) { t.Fatal("invalid atomic GP effect") })
			a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("invalid atomic symbolic effect") })
			emitAtomic(&a, operation, tc.dst, tc.value, tc.base, tc.order)
			a.ScD(X5, X7, X6, AtomicRelease)
		}
	}
	for _, operation := range []string{"sc.w", "sc.d"} {
		var a Asm
		a.ObserveGPWrites(func(uint32) { t.Fatal("invalid SC source GP effect") })
		a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("invalid SC source symbolic effect") })
		emitAtomic(&a, operation, X5, 32, X6, AtomicRelease)
		if a.Err() == nil || len(a.B) != 0 {
			t.Fatal("invalid SC source was emitted")
		}
		a.ScD(X5, X7, X6, AtomicRelease)
	}
}
