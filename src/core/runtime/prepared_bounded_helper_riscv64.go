// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64

package runtime

import (
	"fmt"

	"github.com/wago-org/wago/internal/runtimebridge"
)

func (p *PreparedHostScalarCall) CallBoundedHelper(_ runtimebridge.HostScalarCallAccess, _, _ uint32, _ HostCall) error {
	return fmt.Errorf("jit: bounded helper calls are not supported on riscv64")
}
