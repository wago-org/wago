// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "github.com/wago-org/wago/internal/buildfeatures"

// These launchers always stop before committing a live instance.
const standaloneStopsBeforeInstance = buildfeatures.StandaloneUnboundFunctionImport != "" ||
	buildfeatures.StandaloneUnboundStorageDiagnostic != "" || buildfeatures.StandaloneUnboundTagImport != ""

// Storage preflight and function import failures occur before collector preparation.
const standaloneStopsBeforeReferencePreparation = buildfeatures.StandaloneUnboundFunctionImport != "" ||
	buildfeatures.StandaloneUnboundStorageDiagnostic != ""
