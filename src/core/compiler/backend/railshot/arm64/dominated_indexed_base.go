// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var dominatedIndexedBaseEnabled = os.Getenv("WAGO_ARM64_NO_DOMINATED_INDEXED_BASE") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE") != "0"

var dominatedIndexedKeyEnabled = os.Getenv("WAGO_ARM64_NO_DOMINATED_INDEXED_KEY") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_KEY") != "0"
