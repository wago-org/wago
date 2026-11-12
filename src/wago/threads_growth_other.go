// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func threadedGrowthHelpers(*wasm.Module) bool { return false }
func threadedMemorySizeGrowSupported() bool   { return false }
func (*Instance) dispatchAtomicMemoryGrow([]uint64) (uint32, error) {
	return 0, fmt.Errorf("shared memory growth helper is unavailable")
}
