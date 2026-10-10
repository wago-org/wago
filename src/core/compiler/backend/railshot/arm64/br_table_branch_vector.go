//go:build arm64

package arm64

import "os"

var brTableBranchVectorEnabled = os.Getenv("WAGO_ARM64_NO_BR_TABLE_BRANCH_VECTOR") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_BR_TABLE_BRANCH_VECTOR") != "0"

// A uniform executable vector needs no offset load: every entry is one B.
// Restrict the first cover to the existing small unique-table cohort, avoiding
// duplicate-heavy edge-list growth and any branch-value/handler setup.
func (f *fn) canBranchVector(labels []uint32, def uint32) bool {
	if !f.opt(optBrTableBranchVector) || f.compactNative() || f.usesCalls || f.moduleEH || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() || len(labels) < brTableJumpMin || !brTableSmallLabelsUnique(labels) {
		return false
	}
	for _, label := range labels {
		if !f.branchVectorTarget(label) {
			return false
		}
	}
	return f.branchVectorTarget(def)
}

func (f *fn) branchVectorTarget(label uint32) bool {
	if uint64(label) >= uint64(len(f.ctrl)) {
		return false
	}
	fr := &f.ctrl[len(f.ctrl)-1-int(label)]
	return fr.kind != cfFunc && fr.branchArity() == 0 && !fr.has(ctrlRegMerge1)
}

func (f *fn) emitBranchVector(labels []uint32, def uint32, index Reg, emitCase func(uint32)) {
	f.a.CmpImm32(index, uint32(len(labels))) // bounded unique cohort is <=32.
	defaultSite := f.a.Bcond(condAE)
	adr := f.a.Adr(X16)
	f.recordPCRelative(adr)
	// W-index extension consumes exactly the Wasm i32, even with dirty high bits.
	if !f.a.AddExtUXTWShift(X17, X16, index, 2) {
		panic("invalid branch-vector scale")
	}
	f.a.Br(X17)
	// Keep the default nearby, independent of the selected table entry.
	f.patchBranch19(defaultSite, f.a.Len())
	emitCase(def)
	start := f.a.Len()
	f.a.PatchAdr(adr, start)
	for _, label := range labels {
		emitCase(label)
	}
	// Indirect entry slots must remain exactly four bytes apart. Mark them
	// opaque so later size-stable peepholes cannot rewrite their entry points.
	f.recordOpaqueData(start, f.a.Len())
	f.stats.peep("br-table-jump")
	f.stats.peep("br-table-branch-vector")
	f.unreachable = true
}
