// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_guardpage && (((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo))

package runtime

import "fmt"

func validateGuardedJobMemorySizes(initialBytes, maxBytes int) error {
	if err := validateJobMemorySizes(initialBytes, maxBytes); err != nil {
		return err
	}
	if uint64(initialBytes) > uint64(maxClassicLinMemBytes) || uint64(maxBytes) > uint64(maxClassicLinMemBytes) {
		return fmt.Errorf("runtime: guarded linear-memory size exceeds memory32 limit: initial %d maximum %d", initialBytes, maxBytes)
	}
	const wasmPageBytes = 1 << 16
	if initialBytes%wasmPageBytes != 0 || maxBytes%wasmPageBytes != 0 {
		return fmt.Errorf("runtime: guarded linear-memory size is not wasm-page aligned: initial %d maximum %d", initialBytes, maxBytes)
	}
	return nil
}
