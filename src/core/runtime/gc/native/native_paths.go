// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import (
	"os"
	"runtime"
	"strings"

	"github.com/wago-org/wago/internal/buildfeatures"
)

// Collector algorithms operate on the shared checked handle and object layout.
// Both native backends use this layout; instruction selection remains separate.
const nativeCollectorFastPaths = buildfeatures.StandaloneNativeCollector &&
	(runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")

var denseNurseryPregrowEnabled bool

// WAGO_GC_SUBTYPE_INTERVALS=0 restores validated parent-chain traversal.
var gcSubtypeIntervalsEnabled bool

func init() {
	if nativeCollectorFastPaths {
		denseNurseryPregrowEnabled = os.Getenv("WAGO_"+strings.ToUpper(runtime.GOARCH)+"_NO_DENSE_NURSERY_PREGROW") != "1"
	}
	if buildfeatures.StandaloneNativeCollector && buildfeatures.GC != 0 {
		gcSubtypeIntervalsEnabled = os.Getenv("WAGO_GC_SUBTYPE_INTERVALS") != "0"
	}
}
