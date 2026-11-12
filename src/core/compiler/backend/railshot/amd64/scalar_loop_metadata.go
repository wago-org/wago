// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// Bound proof work as well as copied code. Pending exits and relocations can
// include an arbitrarily large prefix from earlier loops in the same function.
// Exhaustion retains ordinary lowering without modifying code or metadata.
const scalarLoopMetadataLimit = 256

type scalarLoopMetadataBudget struct {
	remaining int
	stats     *CodegenStats
}

func (f *fn) newScalarLoopMetadataBudget() scalarLoopMetadataBudget {
	return scalarLoopMetadataBudget{remaining: scalarLoopMetadataLimit, stats: f.stats}
}

func (b *scalarLoopMetadataBudget) take(n int) bool {
	if n > b.remaining {
		b.stats.peep("scalar-loop-metadata-limit")
		return false
	}
	b.remaining -= n
	return true
}
