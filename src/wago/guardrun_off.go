// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !(darwin && arm64 && !tinygo && !wago_target_tinygo) && (!wago_guardpage || !(((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)))

package wago

import (
	"fmt"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// newGuardedJobMemory is unavailable when this target lacks built-in or tagged
// guard-page support; the config layer normally rejects this path first.
func newGuardedJobMemory(int, int) (*wruntime.JobMemory, error) {
	return nil, fmt.Errorf("signals-based bounds checks require Darwin ARM64 or a binary built with -tags wago_guardpage")
}

func callNative(_ *Compiled, eng *wruntime.Engine, jm *wruntime.JobMemory, refreshControl bool, entry uintptr, serArgs, trap, results []byte) error {
	if err := refreshNativeControl(refreshControl, eng, jm, trap); err != nil {
		return err
	}
	if preparedCallEnabled {
		err := eng.CallPrepared(entry, serArgs, jm.LinMemBase(), trap, results)
		if err != nil {
			jm.ClearEHHandler()
		}
		return err
	}
	return eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
}
