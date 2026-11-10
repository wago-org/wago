// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package jitprofile

import "golang.org/x/sys/unix"

const Clock = "monotonic"

func Now() uint64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		panic(err)
	}
	return uint64(ts.Nano())
}

func threadID() uint32 { return uint32(unix.Gettid()) }
