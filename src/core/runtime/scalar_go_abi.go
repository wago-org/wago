// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64)) && !tinygo

package runtime

import (
	"strconv"
	"strings"
)

// Unknown internal Go ABIs retain the ordinary host loop.
func scalarGoABI(version string) bool {
	if !strings.HasPrefix(version, "go1.") {
		return false
	}
	minor := strings.SplitN(strings.TrimPrefix(version, "go1."), ".", 2)[0]
	value, err := strconv.Atoi(minor)
	return err == nil && value >= 22 && value <= 27
}
