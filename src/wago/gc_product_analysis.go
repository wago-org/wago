// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "os"

// Feature requirements are already proved by validation.
// It avoids product hashes and opcode walks for modules that cannot use GC.
var gcProductRequiredGateEnabled = os.Getenv("WAGO_GC_PRODUCT_GATE") != "0"

func gcProductAnalysisNeeded(required CoreFeatures) bool {
	return !gcProductRequiredGateEnabled || required.IsEnabled(CoreFeatureGC)
}
