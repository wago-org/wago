// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var brTableRunsEnabled = os.Getenv("WAGO_ARM64_NO_BR_TABLE_RUNS") != "1"

// A small number of contiguous equal-target runs needs no table, indirect
// dispatch or index scratch. Admission finishes before emitting any code.
func (f *fn) tryBranchTableRuns(labels []uint32, def uint32, index Reg, emitCase func(uint32)) bool {
	if !brTableRunsEnabled || !f.opt(optBranchFold) || f.compactNative() || f.usesCalls || f.moduleEH || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() || len(labels) < 8 || len(labels) > 256 {
		return false
	}
	var runs [3]struct {
		end   int
		label uint32
	}
	n := 0
	for i, label := range labels {
		if i == 0 || label != labels[i-1] {
			if n == len(runs) {
				return false
			}
			fr := &f.ctrl[f.sourceLabelFrame(label)]
			if (fr.kind != cfBlock && fr.kind != cfIf) || fr.branchArity() != 0 || fr.has(ctrlRegMerge1) {
				return false
			}
			runs[n].label = label
			n++
		}
		runs[n-1].end = i + 1
	}
	// Compare chains for ordinary small or fragmented tables retain their policy.
	if len(labels) < 2*n {
		return false
	}
	for _, run := range runs[:n] {
		// A trailing default run and all out-of-range indexes share the default.
		if run.end == len(labels) && run.label == def {
			break
		}
		f.a.CmpImm32(index, uint32(run.end))
		fr := &f.ctrl[f.sourceLabelFrame(run.label)]
		f.convergeBranchLocals(fr)
		f.condBranchJump(fr, condB)
	}
	emitCase(def)
	f.stats.peep("br-table-runs")
	f.unreachable = true
	return true
}
