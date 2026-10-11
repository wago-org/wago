//go:build arm64

package arm64

import "os"

var takenZeroBranchEnabled = os.Getenv("WAGO_ARM64_NO_TAKEN_ZERO_BRANCH") != "1"

// The taken edge of CBZ/CBNZ already determines a second same-width test of
// the same register at its direct destination. Bypass that second test without
// changing any word on intervening or alternative paths. In particular, this
// does not speculate loads or assume the register survives the skipped body.
func (f *fn) threadTakenZeroBranches(b []byte, n int, targets []uint64) {
	if f.opaqueFragments || f.moduleEH || len(f.customInstructions) != 0 {
		return
	}
	for pc := 0; pc+4 <= n; pc += 4 {
		w := rdWord(b, pc)
		if w&0x7e000000 != 0x34000000 {
			continue
		}
		target, ok := branchTarget(pc, w)
		if !ok || target < 0 || target+4 > n || target == pc {
			continue
		}
		next := rdWord(b, target)
		// Ignore only displacement and polarity: width and register must match.
		if next&0x7e000000 != 0x34000000 || (w^next)&0xfe00001f != 0 {
			continue
		}
		end := target + 4 // inverse test must fall through on the known edge
		if (w^next)&0x01000000 == 0 {
			end, ok = branchTarget(target, next)
		}
		if !ok || end < 0 || end >= n || end == pc || end == target {
			continue
		}
		delta := (end - pc) / 4
		if delta < -(1<<18) || delta >= 1<<18 {
			continue
		}
		wrWord(b, pc, w&^uint32(0x7ffff<<5)|(uint32(delta)&0x7ffff)<<5)
		// Later word-replacing peepholes must see the new incoming edge.
		branchTargetAdd(targets, end, n)
		f.stats.peep("taken-zero-branch")
	}
}
