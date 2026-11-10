// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux

package gocommand

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

func waitDirectExit(pid int) error {
	var info unix.Siginfo
	for {
		// WNOWAIT observes the direct child without freeing its PID/PGID.
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}
