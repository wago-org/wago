// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package shared

const scalarGraphChecks = false

type scalarGraphSnapshot struct{}

// Hooks are package functions so ordinary ScalarState method metadata stays
// unchanged even when the linker erases all guarded calls.

func checkGraphBegin(_ *ScalarState, t ScalarTarget) ScalarTarget { return t }
func checkGraphComplete(_ *ScalarState)                           {}
func checkGraphEnd(_ *ScalarState)                                {}
func checkGraphAdd(*ScalarState, scalarID)                        {}
func checkGraphSeed(*ScalarState, scalarID)                       {}
func checkGraphUse(*ScalarState, scalarID)                        {}
func checkGraphInputs(*ScalarState, scalarID, scalarID)           {}
func checkGraphDefine(*ScalarState, scalarID, uint8)              {}
func checkGraphResult(*ScalarState, scalarID)                     {}
func checkGraphElse(_ *ScalarState)                               {}
func checkGraphRestoreCarrier(*ScalarState, bool, uint8)          {}
func checkGraphRestore(*ScalarState, int) scalarGraphSnapshot {
	return scalarGraphSnapshot{}
}
func checkGraphRestored(*ScalarState, scalarGraphSnapshot) {}
