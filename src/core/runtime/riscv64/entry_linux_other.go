// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !riscv64 && !tinygo

package riscv64

func flushICache([]byte, int) error                                           { return ErrUnavailable }
func nativeCall(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) { return 0, 2 }

const nativeAvailable = false
