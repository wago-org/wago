// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var straightLineBlockEnabled = true

func (f *fn) straightLineBlock(r *wasm.Reader, kind ctrlKind, params, results int, result machineType, signature wasm.Reader) bool {
	if !f.opt(optStraightLineBlock) || kind != cfBlock || f.unreachable || params > 8 || results > 1 ||
		(results != 0 && result != mtI32 && result != mtI64 && result != mtF32 && result != mtF64 && result != mtV128) ||
		f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 {
		return false
	}
	// Parameterized blocks use type indexes. Validate their Wasm types so an
	// integer machine carrier cannot admit a reference or floating parameter.
	if params != 0 {
		index, err := signature.I64()
		if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)) {
			return false
		}
		ft, ok := f.m.TypeFunc(uint32(index))
		if !ok {
			return false
		}
		for _, typ := range ft.Params {
			if typ != wasm.I32 && typ != wasm.I64 {
				return false
			}
		}
		for _, typ := range ft.Results {
			if typ != wasm.I32 && typ != wasm.I64 {
				return false
			}
		}
	}
	if f.makesCalls || f.inlineTargets.empty() {
		return shared.IsStraightLineBlock(r, f.classifier)
	}
	return shared.IsStraightLineBlockWithSplicedCalls(r, f.classifier, func(index uint32) bool {
		t := f.selectedInlineTarget(index)
		// The select class is a complete read-only condition and local-only
		// pair if; every private path falls through its inlined boundary.
		return t != nil && (t.resultTypeEnd-t.localTypeEnd == 2 && f.pureIntegerParams(t) || t.tupleSelect() ||
			f.inlineDepth == 0 && f.inlineTupleLoopFallthroughState(t))
	})
}

// Ordinary blocks have no header branch site (-1). This distinct sentinel
// records the proven fallthrough-only frame without consuming packed flag bits.
const straightLineBlockSite = -2

func (fr *ctrlFrame) isStraightLineBlock() bool {
	return fr.kind == cfBlock && fr.controlSite == straightLineBlockSite
}
