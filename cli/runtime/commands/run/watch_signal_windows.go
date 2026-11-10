// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows && !wago_lean

package run

import "os"

func watchedSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func watchedContinueSignal(os.Signal) bool { return false }

func watchedSignalExitCode(os.Signal) int { return 130 }
