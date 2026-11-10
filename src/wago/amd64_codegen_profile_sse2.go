// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_amd64_sse2

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// The immutable baseline build profile enables conformance testing on modern
// hosts and portable artifact generation. It only removes optional features.
func selectedAMD64CompileFeatures(_ shared.AMD64Features) shared.AMD64Features { return 0 }
