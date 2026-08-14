//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestFunctionLocalTrapUnwindSharingIsNotProfitableAMD64(t *testing.T) {
	a := &amd64.Asm{}
	start := a.Len()
	a.Load64(RSP, RBX, -offTrapStackReentry)
	a.Ret()
	unwindBytes := a.Len() - start

	b := &amd64.Asm{}
	b.JmpPlaceholder()
	jumpBytes := b.Len()
	if unwindBytes != 5 || jumpBytes != 5 {
		t.Fatalf("trap unwind/jump bytes = %d/%d, want 5/5", unwindBytes, jumpBytes)
	}
	// N local tails cost N*5 bytes; N jumps plus one shared five-byte tail
	// cost N*5+5. Sharing can never cross over without a shorter transfer.
	for groups := 2; groups <= 32; groups++ {
		local := groups * unwindBytes
		shared := groups*jumpBytes + unwindBytes
		if shared <= local {
			t.Fatalf("%d groups unexpectedly cross over: local=%d shared=%d", groups, local, shared)
		}
	}
}

func TestSizeSharesCompleteTrapBodyAMD64(t *testing.T) {
	before := sharedTrapBodyEnabled
	t.Cleanup(func() { sharedTrapBodyEnabled = before })
	emit := func(enabled bool) (int, *CodegenStats) {
		sharedTrapBodyEnabled = enabled
		a := &amd64.Asm{}
		sc := &scratch{}
		stats := &CodegenStats{}
		f := fn{
			a:      a,
			sc:     sc,
			stats:  stats,
			policy: CodegenPolicy{Objective: OptimizeSize},
		}
		for code := uint32(1); code <= 3; code++ {
			branch := a.JccPlaceholder(condNE)
			sc.trapSites[code] = append(sc.trapSites[code], trapSite{
				branch: branch, function: 4, pc: code * 10,
			})
		}
		f.emitTrapStubs()
		return a.Len(), stats
	}

	rollbackBytes, _ := emit(false)
	sharedBytes, stats := emit(true)
	if sharedBytes >= rollbackBytes {
		t.Fatalf("shared trap bytes = %d, rollback = %d; want shrink", sharedBytes, rollbackBytes)
	}
	if stats.Peephole["shared-trap-body"] != 1 || stats.TrapStubs != 3 || stats.TrapGroups != 3 {
		t.Fatalf("shared trap stats = stubs:%d groups:%d peep:%v", stats.TrapStubs, stats.TrapGroups, stats.Peephole)
	}
}
