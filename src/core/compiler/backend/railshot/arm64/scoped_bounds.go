// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var scopedBoundsEnabled = os.Getenv("WAGO_ARM64_NO_SCOPED_BOUNDS") != "1"

type scopedBoundsCert struct {
	cert  boundsCert
	depth uint16
}

// Entry checks dominate both arms and every loop iteration. Keep only local
// sources with no write anywhere in this scope; facts learned inside the scope
// remain in the ordinary cache and are never promoted through a join.
func (f *fn) prepareScopedBounds(r *wasm.Reader) {
	if !f.boundsFacts || !f.opt(optScopedBounds) || f.unreachable || f.moduleEH || f.threadedMemory0 || f.interruptible || len(f.customInstructions) != 0 || f.inlineDepth != 0 || f.localBase != 0 || f.nLocals > 64 || len(f.ctrl) >= 65535 || r.BytesLeft() > 4096 {
		return
	}
	memory, ok := f.memoryType(0)
	if !ok || memory.Limits.Addr64 {
		return
	}
	// Bound lookahead and retained state independently of nesting/local counts.
	var pending [8]boundsCert
	n := 0
	for _, c := range f.boundsCerts {
		if c.kind == 1 {
			pending[n] = c
			n++
		}
	}
	if n == 0 {
		return
	}
	var modified [64]uint16
	sets, hasCall := scanLoopSetLocals(r, f.classifier, modified[:0], nil)
	if sets == nil || hasCall {
		return
	}
	for _, c := range pending[:n] {
		if loopSetsLocal(sets, c.idx) {
			continue
		}
		for i := range f.scopedBounds {
			if f.scopedBounds[i].depth == 0 {
				f.scopedBounds[i] = scopedBoundsCert{cert: c, depth: uint16(len(f.ctrl) + 1)}
				break
			}
		}
	}
}

func (f *fn) scopedBoundsCovers(kind uint8, idx uint32, extent int32) bool {
	if kind != 1 || !f.opt(optScopedBounds) {
		return false
	}
	for _, s := range f.scopedBounds {
		if s.depth != 0 && int(s.depth) <= len(f.ctrl) && s.cert.kind == kind && s.cert.idx == idx && extent <= s.cert.extent {
			f.stats.peep("scoped-bounds-certificate")
			return true
		}
	}
	return false
}

func (f *fn) releaseScopedBounds(depth int) {
	for i := range f.scopedBounds {
		if int(f.scopedBounds[i].depth) == depth {
			f.scopedBounds[i] = scopedBoundsCert{}
		}
	}
}

// A conditional branch leaves the fallthrough path dominated by the checks
// immediately preceding it. A normal flush clears these facts for joins, but
// the fallthrough may retain local certificates whose source did not change.
// The usual control-boundary invalidation still applies at the eventual join.
var conditionalBoundsEnabled = os.Getenv("WAGO_ARM64_NO_CONDITIONAL_BOUNDS") != "1"

func (f *fn) conditionalBoundsEligible() bool {
	return f.opt(optConditionalBounds) && f.conditionalBoundsSized && f.boundsFacts && !f.moduleEH && !f.threadedMemory0 && !f.interruptible && len(f.customInstructions) == 0
}

func (f *fn) conditionalBoundsSnapshot(setLocal int) [8]boundsCert {
	saved := f.boundsCerts
	for i := range saved {
		if saved[i].kind != 1 || int(saved[i].idx) == setLocal {
			saved[i] = boundsCert{}
		}
	}
	return saved
}

func (f *fn) restoreConditionalBounds(saved [8]boundsCert) {
	for _, c := range saved {
		if c.kind != 0 {
			f.boundsCertUpdate(c.kind, c.idx, c.extent)
		}
	}
}
