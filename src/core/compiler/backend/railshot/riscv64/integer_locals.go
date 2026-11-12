// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func publicFunctionLocalLimit(opts CompileOptions) uint32 {
	if opts.MaxFunctionLocals != 0 {
		return opts.MaxFunctionLocals
	}
	return wasm.DefaultMaxFunctionLocals
}

// Public validation has already applied the configured local limit. Bound
// carrier storage by the native frame before allocating it for a large function.
// The private compiler keeps its original 256-local limit.
func integerLocalCount(fn *wasm.Func, ft *wasm.CompType, public bool) (int, error) {
	limit := 256
	if public {
		limit = (MaxFrameBytes - 16) / 8
	}
	n := len(ft.Params)
	if n > limit {
		return 0, unsupported("local frame bound")
	}
	for _, run := range fn.Locals.Runs {
		if uint64(run.Count) > uint64(limit-n) {
			return 0, unsupported("local frame bound")
		}
		n += int(run.Count)
	}
	return n, nil
}
