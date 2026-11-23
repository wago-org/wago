//go:build wago_regalloccheck

package arm64

import (
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func TestFcselDestinationEffects(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, sources := range [][2]Reg{{9, 10}, {8, 10}, {9, 8}, {8, 8}} {
			var a Asm
			var effects []regalloccheck.Effect
			var writes []uint32
			a.ObserveRegalloc(func(e regalloccheck.Effect) { effects = append(effects, e) })
			a.ObserveFPWrites(func(mask uint32) { writes = append(writes, mask) })
			a.Fcsel(8, sources[0], sources[1], CondEQ, wide)
			want := regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Register(regalloccheck.FP, 8), Size: 16}
			if len(effects) != 1 || effects[0] != want {
				t.Errorf("f64=%v sources=%v: effects=%+v, want one %+v", wide, sources, effects, want)
			}
			if len(writes) != 1 || writes[0] != 1<<8 {
				t.Errorf("f64=%v sources=%v: writes=%v, want one destination write", wide, sources, writes)
			}
		}
	}
}
