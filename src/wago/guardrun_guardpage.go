// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_guardpage && (((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo))

package wago

import wruntime "github.com/wago-org/wago/src/core/runtime"

// newGuardedJobMemory installs the guard-page trap handler (idempotent) and
// returns a reservation-backed linear memory for signals-based bounds checks.
func newGuardedJobMemory(linBytes, maxBytes int) (*wruntime.JobMemory, error) {
	if err := wruntime.InstallGuardTrapHandler(); err != nil {
		return nil, err
	}
	return wruntime.AcquireJobMemoryGuarded(linBytes, maxBytes)
}

func callNative(c *Compiled, eng *wruntime.Engine, jm *wruntime.JobMemory, refreshControl bool, entry uintptr, serArgs, trap, results []byte) error {
	if err := refreshNativeControl(refreshControl, eng, jm, trap); err != nil {
		return err
	}
	if c.boundsMode != BoundsChecksSignalsBased && preparedCallEnabled {
		return eng.CallPrepared(entry, serArgs, jm.LinMemBase(), trap, results)
	}
	if c.boundsMode == BoundsChecksSignalsBased {
		return eng.CallGuarded(entry, serArgs, jm.LinMemBase(), trap, results, jm)
	}
	return eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
}
