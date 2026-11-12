// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || wago_standalone || tinygo || wago_target_tinygo

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotConfigureFrontendTable64(*frontend.Features) {}

func validateRV64GenericTable64Source(*wasm.Module) error {
	return fmt.Errorf("compile: generic RV64 table64 backend is unavailable")
}
