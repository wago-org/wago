// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

// LoopLocal is the current source-local cell of one bounded loop candidate.
// Initial and Value identify candidate-owned expressions, never allocator nodes.
// Replacing Value does not change expressions captured by an earlier local.get.
// Written cells remain required outputs even when a short lookahead sees no use.
// Type uses the target's existing lowered type; no state survives the candidate.
type LoopLocal[Type ~uint8, Value ~uint8 | ~int] struct {
	Index uint32
	Type  Type
	// Step is a proved modular i32 induction stride; zero means unclassified.
	Step           uint32
	Initial, Value Value
	Written        bool
	Order          uint8
}

// Assign records the final value and source assignment order. Order zero is
// available to planners whose scalar continuation publishes all final locals.
func (l *LoopLocal[Type, Value]) Assign(value Value, order uint8) {
	l.Value, l.Written, l.Order = value, true, order
}

// FindLoopLocal searches only the admitted relevant-local cells. Callers bound
// that slice independently of the source function's total local count.
func FindLoopLocal[Type ~uint8, Value ~uint8 | ~int](locals []LoopLocal[Type, Value], index uint32) int {
	for i := range locals {
		if locals[i].Index == index {
			return i
		}
	}
	return -1
}
