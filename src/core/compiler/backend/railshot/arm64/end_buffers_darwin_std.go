// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && darwin && !tinygo

package arm64

// Bound the branch-offset backing retained by each worker.
const (
	maxRetainedEndsBufs     = 32
	maxRetainedEndsBufSites = 512
)
