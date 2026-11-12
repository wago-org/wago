// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingRuntimeTypedTableCallWire(data []byte) bool { return false }

// AdmitGCTypeSubtypingRuntimeTypedTableCallSource returns the exact-wire
// provenance witness for official gc/type-subtyping.19.wasm.
func AdmitGCTypeSubtypingRuntimeTypedTableCallSource(data []byte) bool { return false }

func admitRV64GCTypeSubtypingRuntimeTypedTableCall(m *wasm.Module) bool { return false }
