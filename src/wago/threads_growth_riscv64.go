// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func threadedGrowthHelpers(m *wasm.Module) bool {
	if m == nil || m.MemCount() != 1 {
		return false
	}
	memory, ok := m.MemoryType(0)
	if !ok || !memory.Shared {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, fn := range m.Code {
		r := wasm.ReaderFrom(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var imm wasm.InstructionImmediate
			if err = classifier.ClassifyInto(&r, op, &imm); err != nil {
				return false
			}
			if op == 0x40 {
				return true
			}
		}
	}
	return false
}

func threadedMemorySizeGrowSupported() bool { return true }
