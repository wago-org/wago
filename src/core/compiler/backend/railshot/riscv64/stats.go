// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

// collectFinalizedStats runs only after the whole image has been staged and its
// deferred ownership transfer has succeeded. FunctionInfo contains final framed
// sizes; fixed-width direct-call patching cannot change them. The compiler bounds
// this directory by directory byte budgets before lowering. Two contiguous allocations
// own the common pointer-shaped result, with no code or compiler scratch retained.
func collectFinalizedStats(c *Compiled, destination *ModuleStats) {
	if !diagnosticsEnabled || destination == nil {
		return
	}
	var snapshot ModuleStats
	if len(c.functions) != 0 {
		functions := make([]CodegenStats, len(c.functions))
		snapshot.Funcs = make([]*CodegenStats, len(functions))
		for i, info := range c.functions {
			functions[i].FuncIdx = i
			functions[i].CodeBytes = info.CodeBytes
			functions[i].FrameBytes = info.FrameBytes
			functions[i].GCCodeBytes.Total = info.CodeBytes
			snapshot.Funcs[i] = &functions[i]
		}
	}
	c.installProfileStats(&snapshot)
	*destination = snapshot
}
