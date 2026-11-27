// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (arm64 && (linux || darwin || (windows && !tinygo))) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

func useBoundedLightDirectInt(light bool) bool {
	return directIntBoundedLightForImage && (!directIntBoundedFullForImage || light)
}

func useUnboundedLightDirectInt(light bool) bool {
	return directIntUnboundedLightForImage && (!directIntUnboundedFullForImage || light)
}
