// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux && !darwin && !windows

package cache

import "errors"

func cleanPluginBuilds(string) (Result, error) {
	return Result{}, errors.New("secure global plugin cache cleanup is unsupported on this platform")
}

func sizePluginBuilds(string) (int64, error) {
	return 0, errors.New("secure global plugin cache sizing is unsupported on this platform")
}
