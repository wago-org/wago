// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && wago_no_gc && wago_no_exception_handling

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type integerGCFrameState struct{}

func (*integerEmitter) prepareCollectorOperands(*wasm.Func, bool) error { return nil }

func (*integerEmitter) initializeGCFrameRoots(_, _, allocations int) error {
	if allocations != 0 {
		return unsupported("SWAR allocating GC requires exact frame roots")
	}
	return nil
}

func (*integerEmitter) finishScalarGCFrameRoots() error { return nil }

func (*integerEmitter) finishSWARGCFrameRoots(int) error { return nil }

func (*integerEmitter) recordDeadGCFrameSites(*wasm.ModuleInstructionClassifier, wasm.Reader, byte) error {
	return nil
}

func (*integerEmitter) recordGCFrameCallsite(int, []bool) {}

func (*integerEmitter) recordGCFrameCallsiteAdjusted(int, uint32, []bool) {}

func (*integerEmitter) recordGCFrameSafepoint([]bool) uint32 { return 0 }

func (*integerEmitter) recordGCFrameSafepointAt([]bool, uint32) uint32 { return 0 }
