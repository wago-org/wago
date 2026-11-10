// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && (amd64 || arm64 || riscv64)

package runtime

// Unix's syscall mapper owns the original slice until Munmap succeeds.
func closeOwnedMapping(mem *[]byte, base, size uintptr) error {
	var err error
	if base != 0 {
		err = munmapRange(base, size)
	} else {
		err = munmap(*mem)
	}
	if err == nil {
		*mem = nil
	}
	return err
}
