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
	"slices"
)

func rv64ReferenceResourceImportCopy(c *Compiled) *Compiled {
	v := rv64ReferenceImportCopy(c)
	v.GlobalImports = slices.Clone(v.GlobalImports)
	v.GlobalExports = maps.Clone(v.GlobalExports)
	v.memoryDir.exports = maps.Clone(v.memoryDir.exports)
	v.validateMemo.importModuleEnds = slices.Clone(c.loadValidateMemo().importModuleEnds)
	return v
}
