// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 || arm64

package wago

import "sync"

type executionLease struct {
	tables railshotTableEntryLease
	local  *sync.Mutex
	// private denotes an engine-owned numeric context; no native resource lease.
	private bool
}

func (l executionLease) privateContext() bool      { return l.private }
func privateNumericExecutionLease() executionLease { return executionLease{private: true} }
