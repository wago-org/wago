// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Expansion is restricted to the existing helper-free integer tail-loop proof.
// A per-splice byte budget bounds total growth across all source branches, while
// the depth bound keeps compiler recursion independent of guest recursion.
const (
	inlineTailExpansionBytes = 512
	inlineTailExpansionDepth = 2
)

func (f *fn) tryExpandInlineTail(t *inlineTarget, constants *inlineTailConstants) (bool, error) {
	cost := int(f.inlineTailCopyBytes)
	if constants == nil || !t.tailLoop() || f.inlineTailExpandDepth >= inlineTailExpansionDepth || int(f.inlineTailExpandBytes) < cost {
		return false, nil
	}
	f.inlineTailExpandBytes -= uint16(cost)
	f.inlineTailExpandDepth++
	defer func() { f.inlineTailExpandDepth-- }()
	if err := f.emitInlineTailCopy(t, constants, -1); err != nil {
		return true, err
	}
	f.stats.peep("inline-tail-expand")
	return true, nil
}

func (f *fn) emitInlineTailCopy(t *inlineTarget, constants *inlineTailConstants, argumentStart int) error {
	previousSource := f.inlineTailSourceFrame
	previousArgument := f.inlineTailArgumentStart
	f.inlineTailArgumentStart = argumentStart
	previousConstants := f.inlineTailConstants
	previousCompare := f.inlineTailCompare
	f.inlineTailCompare = inlineTailCompare{active: true, lastCompare: -1, reuseResume: -1}
	previousTrace, previousBase, previousPC := f.traceFuncIdx, f.tracePCBase, f.wasmPC
	if previousConstants != nil && previousConstants.entry != nil {
		if constants == nil {
			constants = &inlineTailConstants{base: f.localBase, blocked: 255}
		}
		constants.entry = previousConstants.entry
	}
	f.inlineTailConstants = constants
	f.traceFuncIdx, f.tracePCBase = uint32(t.globalIdx), t.localDeclBytes
	defer func() {
		f.inlineTailSourceFrame = previousSource
		f.inlineTailArgumentStart = previousArgument
		f.inlineTailConstants = previousConstants
		f.inlineTailCompare = previousCompare
		f.inlineTailCompare.reuseResume = -1
		f.traceFuncIdx, f.tracePCBase, f.wasmPC = previousTrace, previousBase, previousPC
	}()
	// A tail transfer discards source operands below its arguments, but must
	// still realize any pending traps before dropping their deferred roots.
	f.materializePendingTraps()
	outer := &f.ctrl[f.inlineRetFrame]
	f.setDepthTypesWithGCRoots(f.frameBaseTypes(outer), f.frameBaseGCRoots(outer))
	minCtrl := len(f.ctrl)
	results := f.inlineTargets.resultTypes(t)
	frame := ctrlFrame{kind: cfBlock, resultN: len(results), branchN: uint32(len(results)), types: results, res0: t.res0, controlSite: -1, height: f.depth()}
	frame.set(ctrlRegMerge1, outer.has(ctrlRegMerge1))
	f.setFrameBaseTypePrefix(&frame, frame.height)
	f.captureGCFrameShape(&frame)
	f.pushCtrl(&frame)
	// A source function-label branch exits the original inline invocation, not
	// an enclosing source if or this copy's private synthetic boundary.
	f.inlineTailSourceFrame = minCtrl
	if err := f.bodyLoop(wasm.NewReader(t.body), minCtrl); err != nil {
		return err
	}
	if !f.unreachable {
		f.branchToFrame(f.inlineRetFrame)
	}
	f.unreachable = true
	return nil
}
