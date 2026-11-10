// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package project

import "github.com/wago-org/wago/internal/jsonstrict"

const (
	maxProjectMetadataBytes = 4 << 20
	maxProjectJournalBytes  = 12 << 20 // two base64-encoded metadata files plus framing
)

var projectJSONLimits = jsonstrict.Limits{MaxDepth: 64, MaxValues: 100000}
