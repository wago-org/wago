// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage

package runtime

const rv64GuardMemoryEnabled = false

func NewJobMemoryGuarded(int, int) (*JobMemory, error)     { return nil, ErrUnsupported }
func AcquireJobMemoryGuarded(int, int) (*JobMemory, error) { return nil, ErrUnsupported }
func growGuardedHostView(j *JobMemory, _ int) error {
	if j != nil && j.reserveBase == 0 {
		return nil
	}
	return ErrUnsupported
}
