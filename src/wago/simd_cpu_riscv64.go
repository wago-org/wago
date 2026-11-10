// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// V128 values use fixed-width RV64G scalar pair lowering; no RVV or optional
// bit-manipulation extensions are selected. The experimental whole-module
// policy separately bounds supported state and synchronous host combinations.
func architectureSupportsSIMD() bool                             { return true }
func architectureSupportsBMI2() bool                             { return false }
func architectureAMD64BitCountFeatures() uint8                   { return 0 }
func architectureAMD64CPUFeatures() (shared.AMD64Features, bool) { return 0, true }
