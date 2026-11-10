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

func admitRV64GCTypeSubtypingRuntimeFinalityCallCastWire(data []byte) bool { return false }

// AdmitGCTypeSubtypingRuntimeFinalityCallCastSource returns the exact-wire
// provenance witness for official gc/type-subtyping.18.wasm.
func AdmitGCTypeSubtypingRuntimeFinalityCallCastSource(data []byte) bool { return false }

func admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m *wasm.Module) bool { return false }
