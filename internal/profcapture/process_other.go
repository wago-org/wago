// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux && !darwin

package profcapture

import (
	"os/exec"
	"time"
)

func boundProcess(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
