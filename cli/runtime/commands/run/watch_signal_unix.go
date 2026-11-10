// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !wago_lean

package run

import (
	"os"
	"syscall"
)

func watchedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGCONT}
}

func watchedContinueSignal(signal os.Signal) bool { return signal == syscall.SIGCONT }

func watchedSignalExitCode(signal os.Signal) int {
	if value, ok := signal.(syscall.Signal); ok {
		return 128 + int(value)
	}
	return 1
}
