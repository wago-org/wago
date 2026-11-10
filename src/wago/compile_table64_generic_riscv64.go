// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !wago_standalone && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotConfigureFrontendTable64(features *frontend.Features) {
	features.GenericTable64 = features.Table64
}

// The public validator has checked all index widths, table types and operations.
// Native lowering applies its own carrier rules. General table combinations use
// indexed descriptors; they do not depend on an exact module operation shape.
func validateRV64GenericTable64Source(m *wasm.Module) error {
	if m == nil || m.TableCount() == 0 || m.TableCount() > railshot.MaxModuleTables {
		return fmt.Errorf("compile: RV64 table64 directory count exceeds its bound")
	}
	for index := 0; index < m.TableCount(); index++ {
		table, ok := m.TableType(uint32(index))
		if !ok || table.Limits.Min > uint64(^uint32(0)) || table.Limits.HasMax && table.Limits.Max < table.Limits.Min {
			return fmt.Errorf("compile: RV64 table64 %d has invalid native storage limits", index)
		}
	}
	return nil
}
