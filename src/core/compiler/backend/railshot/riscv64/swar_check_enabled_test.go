//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestSWARCheckedPairTransfersAndLargeAddressLimit(t *testing.T) {
	f := target{}
	var state regalloccheck.State
	low, high := state.Seed(regalloccheck.Slot(16), 8), state.Seed(regalloccheck.Slot(24), 8)
	old := f.a.ObserveRegalloc(state.Apply)
	defer func() { f.a.ObserveRegalloc(old) }()
	f.swarLoad(16, swarPair{enc.X5, enc.X6})
	f.swarMove(swarPair{enc.X6, enc.X5}, swarPair{enc.X5, enc.X6})
	f.swarStore(32, swarPair{enc.X6, enc.X5})
	state.Expect("pair low", regalloccheck.Slot(32), low)
	state.Expect("pair high", regalloccheck.Slot(40), high)
	if f.err != nil || f.a.Err() != nil {
		t.Fatal(f.err, f.a.Err())
	}
	// Existing scalar transfer observation cannot establish dynamic address
	// identities. The large-offset half stays unknown, never an invented Copy
	// from the backend's nominal displacement. Future graph coverage must track
	// the real Constant+ADD-SP chain before claiming this path.
	state.Seed(regalloccheck.Slot(2040), 8)
	state.Seed(regalloccheck.Slot(2048), 8)
	var effects []regalloccheck.Effect
	prior := f.a.ObserveRegalloc(func(e regalloccheck.Effect) { effects = append(effects, e); state.Apply(e) })
	f.swarLoad(2040, swarPair{enc.X7, enc.X9})
	f.a.ObserveRegalloc(prior)
	for _, effect := range effects {
		if effect.Kind == regalloccheck.Copy && effect.Src == regalloccheck.Slot(2048) {
			t.Fatal("nominal large offset falsely observed as frame copy")
		}
	}
	if len(effects) == 0 {
		t.Fatal("missing physical effects")
	}
}
