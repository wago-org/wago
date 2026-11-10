// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows && !tinygo && !wago_lean

package run

import (
	"errors"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWatchedWindowsConsoleUnavailableFallsBack(t *testing.T) {
	if err := watchedWindowsInterruptError(nil); err != nil {
		t.Fatalf("successful interrupt = %v", err)
	}
	for _, err := range []error{windows.ERROR_INVALID_HANDLE, windows.ERROR_INVALID_PARAMETER} {
		if !errors.Is(watchedWindowsInterruptError(err), errWatchedGracefulStopUnavailable) {
			t.Fatalf("interrupt error %v did not request forced fallback", err)
		}
	}
	want := errors.New("console failure")
	if got := watchedWindowsInterruptError(want); !errors.Is(got, want) {
		t.Fatalf("interrupt error = %v, want %v", got, want)
	}
}

func detachWatchHelperProcess(*exec.Cmd) {}

func configureWatchTestSupervisor(*watchOptions) {}

func configureWatchHelperForeground(*testing.T) {}
