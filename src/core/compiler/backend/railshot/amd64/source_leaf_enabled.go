//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func checkSourceBegin(f *fn, hostAdapter bool) {
	admit := !hostAdapter && !f.scalarSummary.Eligible && f.singleRegResult && f.skipFence && !f.interruptible &&
		!f.moduleEH && f.gcFrameRoots == nil && len(f.customInstructions) == 0 && len(f.gcTypeLayouts) == 0 &&
		len(f.m.Memories) == 0 && len(f.m.Globals) == 0
	function := int(f.traceFuncIdx) - f.m.ImportedFuncCount()
	// Dispatch a single bounded source family. A second admission must not
	// overwrite a leaf's ResourceLimit or incomplete-contract report.
	if function >= 0 && function < len(f.m.Code) && len(f.m.Code[function].BodyBytes) == 11 {
		f.sourceBranch = shared.BeginSourceBranch(&f.sc.scalar, f.m, function, admit)
		if f.sourceBranch != nil {
			f.sourceRestore = f.ObserveScalarGraph(f.sourceBranch.ObserveEffect, f.sourceBranch.ObserveGPWrites)
		}
	} else {
		f.sourceLeaf = shared.BeginSourceLeaf(&f.sc.scalar, f.m, function, admit)
		if f.sourceLeaf != nil {
			f.sourceRestore = f.ObserveScalarGraph(f.sourceLeaf.ObserveEffect, f.sourceLeaf.ObserveGPWrites)
		}
	}
}

func checkSourceFinishEmission(f *fn) {
	if f.sourceLeaf != nil {
		f.sourceLeaf.EndEmission(f.a.Len())
	}
	if f.sourceBranch != nil {
		f.sourceBranch.EndEmission(f.a.Len())
	}
}

func checkSourceVerify(f *fn) {
	if f.sourceLeaf == nil {
		if f.sourceBranch != nil {
			result := f.sourceBranch.Verify(f.a.B)
			if result.Verdict == regalloccheck.Rejected {
				panic("source allocation verification: " + result.Message)
			}
		}
		return
	}
	result := f.sourceLeaf.Verify(f.a.B, false)
	if result.Verdict == regalloccheck.Rejected {
		panic("source allocation verification: " + result.Message)
	}
}

func checkSourceClose(f *fn) {
	if f.sourceRestore != nil {
		f.sourceRestore()
		f.sourceRestore = nil
	}
	if f.sourceLeaf != nil {
		f.sourceLeaf.Close()
		f.sourceLeaf = nil
	}
	if f.sourceBranch != nil {
		f.sourceBranch.Close()
		f.sourceBranch = nil
	}
}
