// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"maps"
)

func rv64ReferenceResourceCopy(c *Compiled) *Compiled {
	copy := rv64ReferenceStateCopy(c)
	copy.Exports = maps.Clone(copy.Exports)
	copy.GlobalExports = maps.Clone(copy.GlobalExports)
	copy.memoryDir.exports = maps.Clone(copy.memoryDir.exports)
	return copy
}
