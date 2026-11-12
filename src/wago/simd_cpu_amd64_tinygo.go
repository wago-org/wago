// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"os"
)

func architectureAMD64CPUFeatures() (shared.AMD64Features, bool) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0, false
	}
	return amd64LinuxCPUFeatures(data)
}
func architectureSupportsSIMD() bool {
	_, ok := cachedAMD64CPUFeatures()
	return ok
}
func architectureSupportsBMI2() bool {
	f, ok := cachedAMD64CPUFeatures()
	return ok && f.Has(shared.AMD64BMI2)
}
func architectureAMD64BitCountFeatures() uint8 {
	f, ok := cachedAMD64CPUFeatures()
	if !ok {
		return 0
	}
	return f.BitCountCapabilities()
}
