// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

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
