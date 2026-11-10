// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !amd64 && !arm64

package wago

import "sync"

// Preserve the ordinary lease ABI on targets without private numeric entry.
type executionLease struct {
	tables railshotTableEntryLease
	local  *sync.Mutex
}

func (l executionLease) privateContext() bool      { return false }
func privateNumericExecutionLease() executionLease { panic("private numeric entry unavailable") }
