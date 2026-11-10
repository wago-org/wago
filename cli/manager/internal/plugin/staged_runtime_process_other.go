// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux && !darwin && !windows

package plugin

import "os/exec"

// Wago releases target Linux, Darwin, and Windows. Keep unsupported ports
// buildable while CommandContext still bounds the direct validation process.
func runBoundStagedRuntime(command *exec.Cmd) error { return command.Run() }
