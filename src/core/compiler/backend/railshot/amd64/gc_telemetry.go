//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func (f *fn) recordGCBarrierState(state shared.GCBarrierState) {
	switch state {
	case shared.GCBarrierNoBarrier:
		f.stats.peep("gc-barrier-none")
	case shared.GCBarrierYoungParent:
		f.stats.peep("gc-barrier-young-parent")
	case shared.GCBarrierKnownOldChild:
		f.stats.peep("gc-barrier-known-old-child")
	case shared.GCBarrierExistingCard:
		f.stats.peep("gc-barrier-existing-card")
	case shared.GCBarrierCardMark:
		f.stats.peep("gc-barrier-card-mark")
	case shared.GCBarrierSlowBarrier:
		f.stats.peep("gc-barrier-slow")
	}
}

func (f *fn) recordGCOpcodeBytes(sub uint32, n int) {
	if n <= 0 || f.stats == nil {
		return
	}
	switch sub {
	case 0, 1, 6, 7, 8, 9, 10:
		f.stats.addGCAllocationBytes(n)
	}
	switch sub {
	case 2, 3, 4, 5, 11, 12, 13, 14, 15, 16, 17, 18, 19:
		f.stats.addGCHandleResolutionBytes(n)
	}
	switch sub {
	case 20, 21, 22, 23, 24, 25:
		f.stats.addGCTypeCastBytes(n)
	}
	switch sub {
	case 20, 21, 22, 23, 24, 25, 29, 30:
		f.stats.addGCNullCheckBytes(n)
	}
	switch sub {
	case 11, 12, 13, 14, 16, 17, 18, 19:
		f.stats.addGCBoundsCheckBytes(n)
	}
	if f.gcOpcodeBarrier {
		f.stats.addGCBarrierBytes(n)
	}
}
